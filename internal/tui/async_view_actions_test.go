package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func queuedAsyncActionRunner(t *testing.T, hooks AsyncViewActionHooks) (*AsyncViewActionRunner, <-chan func()) {
	t.Helper()
	queued := make(chan func(), 16)
	runner := NewAsyncViewActionRunner(func(fn func()) { queued <- fn }, hooks)
	return runner, queued
}

func waitAsyncAction(t *testing.T, ch <-chan func()) func() {
	t.Helper()
	select {
	case fn := <-ch:
		return fn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued UI completion")
		return nil
	}
}

func TestAsyncViewActionRunnerReturnsImmediatelyAndCompletesOnQueue(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	var mu sync.Mutex
	events := make([]string, 0, 4)
	runner, queued := queuedAsyncActionRunner(t, AsyncViewActionHooks{
		OnStart: func(name string) {
			mu.Lock()
			events = append(events, "start:"+name)
			mu.Unlock()
		},
		OnSuccess: func(name string) {
			mu.Lock()
			events = append(events, "success:"+name)
			mu.Unlock()
		},
		OnComplete: func(name string, err error) {
			mu.Lock()
			if err != nil {
				events = append(events, "complete:error")
			} else {
				events = append(events, "complete:"+name)
			}
			mu.Unlock()
		},
	})
	var completionErr error
	completed := make(chan struct{})
	startedAt := time.Now()
	if !runner.Run("archive", func(ctx context.Context) error {
		close(started)
		<-finish
		return nil
	}, func(err error) {
		completionErr = err
		close(completed)
	}) {
		t.Fatal("Run rejected first action")
	}
	if elapsed := time.Since(startedAt); elapsed > 150*time.Millisecond {
		t.Fatalf("Run blocked for %s; want immediate dispatch", elapsed)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("action did not start asynchronously")
	}
	select {
	case <-completed:
		t.Fatal("completion ran before action finished or UI queue was applied")
	default:
	}
	close(finish)
	waitAsyncAction(t, queued)()
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("completion callback did not run")
	}
	if completionErr != nil {
		t.Fatalf("completion error = %v, want nil", completionErr)
	}
	if runner.InFlight() {
		t.Fatal("runner remained in flight after queued completion")
	}
	mu.Lock()
	gotEvents := append([]string(nil), events...)
	mu.Unlock()
	if want := []string{"start:archive", "success:archive", "complete:archive"}; len(gotEvents) != len(want) || gotEvents[0] != want[0] || gotEvents[1] != want[1] || gotEvents[2] != want[2] {
		t.Fatalf("lifecycle events = %#v, want %#v", gotEvents, want)
	}
}

func TestAsyncViewActionRunnerRejectsReentryAndSerializesCompletion(t *testing.T) {
	firstFinish := make(chan struct{})
	firstStarted := make(chan struct{})
	runner, queued := queuedAsyncActionRunner(t, AsyncViewActionHooks{})
	var completions []string
	var mu sync.Mutex
	if !runner.Run("first", func(context.Context) error {
		close(firstStarted)
		<-firstFinish
		return nil
	}, func(error) {
		mu.Lock()
		completions = append(completions, "first")
		mu.Unlock()
	}) {
		t.Fatal("first Run rejected")
	}
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first action did not start")
	}
	if runner.Run("second", func(context.Context) error { return nil }, func(error) {
		mu.Lock()
		completions = append(completions, "second")
		mu.Unlock()
	}) {
		t.Fatal("second Run accepted while first action was in flight")
	}
	close(firstFinish)
	waitAsyncAction(t, queued)()
	mu.Lock()
	got := append([]string(nil), completions...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "first" {
		t.Fatalf("completions = %#v, want only first", got)
	}
	if runner.Run("third", func(context.Context) error { return nil }, func(error) {
		mu.Lock()
		completions = append(completions, "third")
		mu.Unlock()
	}) == false {
		t.Fatal("runner did not accept action after first completion")
	}
	waitAsyncAction(t, queued)()
	mu.Lock()
	got = append([]string(nil), completions...)
	mu.Unlock()
	if len(got) != 2 || got[1] != "third" {
		t.Fatalf("completions after reentry = %#v, want first/third", got)
	}
}

func TestAsyncViewActionRunnerDropsStaleCompletionAfterInvalidate(t *testing.T) {
	finish := make(chan struct{})
	started := make(chan struct{})
	var completionCount int
	runner, queued := queuedAsyncActionRunner(t, AsyncViewActionHooks{
		OnSuccess:  func(string) { completionCount++ },
		OnComplete: func(string, error) { completionCount++ },
	})
	if !runner.Run("selection", func(context.Context) error {
		close(started)
		<-finish
		return nil
	}, func(error) { completionCount++ }) {
		t.Fatal("Run rejected action")
	}
	<-started
	oldGeneration := runner.Generation()
	newGeneration := runner.Invalidate()
	if newGeneration <= oldGeneration {
		t.Fatalf("generation = %d after invalidate, want greater than %d", newGeneration, oldGeneration)
	}
	if runner.InFlight() {
		t.Fatal("Invalidate left action in flight")
	}
	close(finish)
	select {
	case queuedCompletion := <-queued:
		queuedCompletion()
	case <-time.After(250 * time.Millisecond):
		// A stale operation is allowed to be dropped before it reaches the UI
		// queue; either path must produce no lifecycle callback.
	}
	if completionCount != 0 {
		t.Fatalf("stale completion count = %d, want zero", completionCount)
	}
}

func TestAsyncViewActionRunnerCancellationPropagatesAndDropsResult(t *testing.T) {
	observedCancellation := make(chan struct{})
	var completionCount int
	runner, queued := queuedAsyncActionRunner(t, AsyncViewActionHooks{})
	if !runner.Run("cancel", func(ctx context.Context) error {
		<-ctx.Done()
		close(observedCancellation)
		return ctx.Err()
	}, func(error) { completionCount++ }) {
		t.Fatal("Run rejected action")
	}
	if !runner.Cancel() {
		t.Fatal("Cancel reported no in-flight action")
	}
	select {
	case <-observedCancellation:
	case <-time.After(2 * time.Second):
		t.Fatal("action did not observe context cancellation")
	}
	select {
	case queuedCompletion := <-queued:
		queuedCompletion()
	case <-time.After(250 * time.Millisecond):
		// Cancel can invalidate before the worker attempts to enqueue.
	}
	if completionCount != 0 {
		t.Fatalf("canceled completion count = %d, want zero", completionCount)
	}
	if runner.Cancel() {
		t.Fatal("second Cancel reported an action")
	}
}

func TestAsyncViewActionRunnerReportsErrorExactlyOnceAndRecoversPanic(t *testing.T) {
	actionErr := errors.New("network unavailable")
	var errorsSeen, successSeen, completeSeen, completionSeen int
	runner, queued := queuedAsyncActionRunner(t, AsyncViewActionHooks{
		OnSuccess: func(string) { successSeen++ },
		OnError: func(_ string, err error) {
			if !errors.Is(err, actionErr) {
				t.Errorf("OnError err = %v, want %v", err, actionErr)
			}
			errorsSeen++
		},
		OnComplete: func(string, error) { completeSeen++ },
	})
	if !runner.Run("error", func(context.Context) error { return actionErr }, func(err error) {
		if !errors.Is(err, actionErr) {
			t.Errorf("completion err = %v, want %v", err, actionErr)
		}
		completionSeen++
	}) {
		t.Fatal("Run rejected error action")
	}
	waitAsyncAction(t, queued)()
	if errorsSeen != 1 || successSeen != 0 || completeSeen != 1 || completionSeen != 1 {
		t.Fatalf("error lifecycle = error %d/success %d/complete %d/completion %d, want 1/0/1/1", errorsSeen, successSeen, completeSeen, completionSeen)
	}

	panicRunner, panicQueue := queuedAsyncActionRunner(t, AsyncViewActionHooks{})
	if !panicRunner.Run("panic", func(context.Context) error { panic("boom") }, func(err error) {
		if err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("panic completion err = %v, want panic error", err)
		}
	}) {
		t.Fatal("Run rejected panic action")
	}
	waitAsyncAction(t, panicQueue)()
}

func TestAsyncViewActionRunnerProgressIsQueuedAndStaleSafe(t *testing.T) {
	var progress []string
	var mu sync.Mutex
	runner, queued := queuedAsyncActionRunner(t, AsyncViewActionHooks{
		OnProgress: func(name, message string) {
			mu.Lock()
			progress = append(progress, name+":"+message)
			mu.Unlock()
		},
	})
	if !runner.Run("save", func(context.Context) error {
		runner.Progress("saving")
		return nil
	}, func(error) {}) {
		t.Fatal("Run rejected progress action")
	}
	waitAsyncAction(t, queued)()
	waitAsyncAction(t, queued)()
	mu.Lock()
	got := append([]string(nil), progress...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "save:saving" {
		t.Fatalf("progress = %#v, want one queued progress event", got)
	}
}
