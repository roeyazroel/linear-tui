package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// ErrAsyncViewActionInFlight reports an attempted re-entry while another
// action is still running. Run returns false for this condition; callers that
// need an error value can use Start.
var ErrAsyncViewActionInFlight = errors.New("async view action already in flight")

// ErrAsyncViewActionClosed reports that a runner has been closed and cannot
// accept new work.
var ErrAsyncViewActionClosed = errors.New("async view action runner is closed")

// AsyncViewActionHooks are optional UI lifecycle callbacks. Success, error,
// completion, and progress callbacks are dispatched through the injected UI
// queue. OnStart runs once on the goroutine that accepts Run, before the
// network operation starts.
type AsyncViewActionHooks struct {
	OnStart    func(name string)
	OnProgress func(name, message string)
	OnSuccess  func(name string)
	OnError    func(name string, err error)
	OnComplete func(name string, err error)
}

// AsyncViewActionResult is the immutable result delivered by RunResult.
type AsyncViewActionResult struct {
	Name       string
	Generation uint64
	Err        error
}

// AsyncViewActionToken identifies the generation active when a view action
// was accepted. Invalidate and Cancel advance the generation, making queued
// completions from a closed view or changed selection stale.
type AsyncViewActionToken struct {
	Generation uint64
}

type asyncViewActionState struct {
	name       string
	token      AsyncViewActionToken
	cancel     context.CancelFunc
	completion func(error)
}

// AsyncViewActionRunner runs one network-backed view action at a time. The
// operation always runs off the UI goroutine. Every progress/final callback is
// posted through queueUpdateDraw and re-checks the generation before touching
// view state.
type AsyncViewActionRunner struct {
	mu                 sync.Mutex
	queueUpdateDraw    func(func())
	hooks              AsyncViewActionHooks
	active             *asyncViewActionState
	generation         uint64
	closed             bool
	progressGeneration atomic.Uint64
}

// NewAsyncViewActionRunner creates a runner. The optional hooks argument keeps
// the common constructor concise while allowing a view to install lifecycle
// status/error handlers in one place. A nil queue executes callbacks directly,
// which is useful for embedding and deterministic tests.
func NewAsyncViewActionRunner(queueUpdateDraw func(func()), hooks ...AsyncViewActionHooks) *AsyncViewActionRunner {
	runner := &AsyncViewActionRunner{queueUpdateDraw: queueUpdateDraw}
	if len(hooks) > 0 {
		runner.hooks = hooks[0]
	}
	return runner
}

// NewAsyncViewActionRunnerWithQueue is a descriptive constructor alias.
func NewAsyncViewActionRunnerWithQueue(queueUpdateDraw func(func())) *AsyncViewActionRunner {
	return NewAsyncViewActionRunner(queueUpdateDraw)
}

// SetQueueUpdateDraw replaces the UI dispatch seam. It is intended for wiring
// a runner to App.QueueUpdateDraw or for deterministic test harnesses.
func (runner *AsyncViewActionRunner) SetQueueUpdateDraw(queueUpdateDraw func(func())) {
	if runner == nil {
		return
	}
	runner.mu.Lock()
	runner.queueUpdateDraw = queueUpdateDraw
	runner.mu.Unlock()
}

// SetHooks replaces lifecycle handlers for future events. Existing operations
// use the hooks captured when they were accepted, so a view replacement cannot
// receive an old operation's status event.
func (runner *AsyncViewActionRunner) SetHooks(hooks AsyncViewActionHooks) {
	if runner == nil {
		return
	}
	runner.mu.Lock()
	runner.hooks = hooks
	runner.mu.Unlock()
}

// Run accepts one operation and returns immediately. false means the runner
// is closed or another operation is already in flight. completion receives the
// operation error exactly once only when the action is still current when its
// queued UI callback runs; stale/canceled results are intentionally dropped.
func (runner *AsyncViewActionRunner) Run(name string, operation func(context.Context) error, completion func(error)) bool {
	if runner == nil || operation == nil {
		return false
	}
	name = normalizeAsyncActionName(name)
	runner.mu.Lock()
	if runner.closed || runner.active != nil {
		runner.mu.Unlock()
		return false
	}
	runner.generation++
	token := AsyncViewActionToken{Generation: runner.generation}
	ctx, cancel := context.WithCancel(context.Background())
	state := &asyncViewActionState{
		name:       name,
		token:      token,
		cancel:     cancel,
		completion: completion,
	}
	runner.active = state
	hooks := runner.hooks
	runner.mu.Unlock()

	callAsyncActionHook(func() {
		if hooks.OnStart != nil {
			hooks.OnStart(name)
		}
	})
	go runner.execute(ctx, state, operation, hooks)
	return true
}

// Start is the error-returning form of Run for adapters that want to
// distinguish re-entry from a closed runner.
func (runner *AsyncViewActionRunner) Start(name string, operation func(context.Context) error, completion func(error)) error {
	if runner == nil {
		return ErrAsyncViewActionClosed
	}
	runner.mu.Lock()
	closed := runner.closed
	active := runner.active != nil
	runner.mu.Unlock()
	if closed {
		return ErrAsyncViewActionClosed
	}
	if active {
		return ErrAsyncViewActionInFlight
	}
	if !runner.Run(name, operation, completion) {
		return ErrAsyncViewActionInFlight
	}
	return nil
}

// RunResult is a result-aware adapter around Run. It preserves the same
// nonblocking, one-at-a-time semantics while exposing the accepted generation.
func (runner *AsyncViewActionRunner) RunResult(name string, operation func(context.Context) error, completion func(AsyncViewActionResult)) bool {
	if runner == nil {
		return false
	}
	return runner.Run(name, operation, func(err error) {
		completion(AsyncViewActionResult{Name: normalizeAsyncActionName(name), Generation: runner.Generation(), Err: err})
	})
}

func (runner *AsyncViewActionRunner) execute(ctx context.Context, state *asyncViewActionState, operation func(context.Context) error, hooks AsyncViewActionHooks) {
	err := runAsyncActionSafely(ctx, operation, state.name)
	runner.finish(state, err, hooks)
}

func (runner *AsyncViewActionRunner) finish(state *asyncViewActionState, err error, hooks AsyncViewActionHooks) {
	runner.mu.Lock()
	if !runner.isCurrentLocked(state) {
		runner.mu.Unlock()
		return
	}
	queue := runner.queueUpdateDraw
	runner.mu.Unlock()

	runner.dispatch(queue, func() {
		runner.mu.Lock()
		if !runner.isCurrentLocked(state) {
			runner.mu.Unlock()
			return
		}
		runner.active = nil
		runner.mu.Unlock()

		if err != nil {
			callAsyncActionHook(func() {
				if hooks.OnError != nil {
					hooks.OnError(state.name, err)
				}
			})
		} else {
			callAsyncActionHook(func() {
				if hooks.OnSuccess != nil {
					hooks.OnSuccess(state.name)
				}
			})
		}
		callAsyncActionHook(func() {
			if hooks.OnComplete != nil {
				hooks.OnComplete(state.name, err)
			}
		})
		callAsyncActionHook(func() {
			if state.completion != nil {
				state.completion(err)
			}
		})
	})
}

// Progress posts one progress/status update for the current action. A stale or
// canceled action's progress is discarded just like its final completion.
func (runner *AsyncViewActionRunner) Progress(message string) bool {
	if runner == nil {
		return false
	}
	runner.mu.Lock()
	state := runner.active
	queue := runner.queueUpdateDraw
	hook := runner.hooks.OnProgress
	if state == nil || hook == nil {
		runner.mu.Unlock()
		return false
	}
	runner.progressGeneration.Store(state.token.Generation)
	runner.mu.Unlock()
	runner.dispatch(queue, func() {
		runner.mu.Lock()
		current := runner.isCurrentLocked(state)
		runner.mu.Unlock()
		if !current || runner.progressGeneration.Load() != state.token.Generation {
			return
		}
		callAsyncActionHook(func() { hook(state.name, message) })
	})
	return true
}

// Invalidate cancels the current operation and advances the generation while
// leaving the runner open for a new selection/view action.
func (runner *AsyncViewActionRunner) Invalidate() uint64 {
	if runner == nil {
		return 0
	}
	runner.mu.Lock()
	runner.generation++
	if runner.active != nil {
		runner.active.cancel()
		runner.active = nil
	}
	generation := runner.generation
	runner.mu.Unlock()
	return generation
}

// Cancel cancels the current operation and invalidates its completion. It
// returns true only when an operation was active.
func (runner *AsyncViewActionRunner) Cancel() bool {
	if runner == nil {
		return false
	}
	runner.mu.Lock()
	if runner.active == nil {
		runner.mu.Unlock()
		return false
	}
	runner.generation++
	runner.active.cancel()
	runner.active = nil
	runner.mu.Unlock()
	return true
}

// Close permanently invalidates the runner and cancels any active operation.
func (runner *AsyncViewActionRunner) Close() bool {
	if runner == nil {
		return false
	}
	runner.mu.Lock()
	runner.closed = true
	runner.generation++
	wasActive := runner.active != nil
	if runner.active != nil {
		runner.active.cancel()
		runner.active = nil
	}
	runner.mu.Unlock()
	return wasActive
}

// InFlight reports whether an action is active or waiting for its queued UI
// completion to be applied.
func (runner *AsyncViewActionRunner) InFlight() bool {
	if runner == nil {
		return false
	}
	runner.mu.Lock()
	active := runner.active != nil
	runner.mu.Unlock()
	return active
}

// Generation returns the current invalidation generation.
func (runner *AsyncViewActionRunner) Generation() uint64 {
	if runner == nil {
		return 0
	}
	runner.mu.Lock()
	generation := runner.generation
	runner.mu.Unlock()
	return generation
}

// CurrentToken returns a token suitable for IsCurrent checks by adapters that
// need to coordinate a view's selection or close lifecycle.
func (runner *AsyncViewActionRunner) CurrentToken() AsyncViewActionToken {
	return AsyncViewActionToken{Generation: runner.Generation()}
}

// IsCurrent reports whether token still names the runner's live generation.
func (runner *AsyncViewActionRunner) IsCurrent(token AsyncViewActionToken) bool {
	if runner == nil {
		return false
	}
	runner.mu.Lock()
	current := !runner.closed && runner.generation == token.Generation
	runner.mu.Unlock()
	return current
}

func (runner *AsyncViewActionRunner) isCurrentLocked(state *asyncViewActionState) bool {
	return !runner.closed && runner.active == state && runner.generation == state.token.Generation
}

func (runner *AsyncViewActionRunner) dispatch(queue func(func()), fn func()) {
	if fn == nil {
		return
	}
	if queue == nil {
		callAsyncActionHook(fn)
		return
	}
	// queueUpdateDraw is application-owned. Recovering here prevents a stale
	// modal action from crashing the process if a test or embedding queue is
	// torn down while a worker is posting its completion.
	defer func() { _ = recover() }()
	queue(fn)
}

func runAsyncActionSafely(ctx context.Context, operation func(context.Context) error, name string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("async view action %q panicked: %v", name, recovered)
		}
	}()
	return operation(ctx)
}

func callAsyncActionHook(fn func()) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn()
}

func normalizeAsyncActionName(name string) string {
	if name == "" {
		return "view action"
	}
	return name
}
