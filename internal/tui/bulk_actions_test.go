package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunBulkActionNormalizesTargetsAndReportsProgress(t *testing.T) {
	var (
		mu       sync.Mutex
		progress []BulkActionProgress
		calls    []string
	)

	summary := RunBulkAction(context.Background(), []string{"", "issue-a", "issue-b", "issue-a", "", "issue-c"}, 2,
		func(_ context.Context, issueID string) error {
			mu.Lock()
			calls = append(calls, issueID)
			mu.Unlock()
			return nil
		},
		func(update BulkActionProgress) {
			mu.Lock()
			progress = append(progress, update)
			mu.Unlock()
		},
	)

	wantIDs := []string{"issue-a", "issue-b", "issue-c"}
	if got := bulkActionResultIDs(summary.Results); !bulkActionStringsEqual(got, wantIDs) {
		t.Fatalf("result IDs = %#v, want %#v", got, wantIDs)
	}
	if summary.Succeeded != len(wantIDs) || summary.Failed != 0 {
		t.Fatalf("summary counts = %d succeeded, %d failed; want %d, 0", summary.Succeeded, summary.Failed, len(wantIDs))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != len(wantIDs) {
		t.Fatalf("mutation calls = %#v, want %d calls", calls, len(wantIDs))
	}
	if len(progress) != len(wantIDs) {
		t.Fatalf("progress updates = %d, want %d", len(progress), len(wantIDs))
	}
	for i, update := range progress {
		if update.Total != len(wantIDs) || update.Completed != i+1 {
			t.Errorf("progress[%d] = %#v, want total %d and completed %d", i, update, len(wantIDs), i+1)
		}
		if update.Succeeded != i+1 || update.Failed != 0 {
			t.Errorf("progress[%d] counts = %d succeeded, %d failed; want %d, 0", i, update.Succeeded, update.Failed, i+1)
		}
		if update.Result.IssueID == "" {
			t.Errorf("progress[%d] has empty latest result: %#v", i, update)
		}
	}
}

func TestRunBulkActionBoundsConcurrencyAndPreservesResultOrder(t *testing.T) {
	ctx := context.Background()
	releaseA := make(chan struct{})
	bStarted := make(chan struct{})
	cStarted := make(chan struct{})
	var bOnce, cOnce sync.Once
	var active, maxActive atomic.Int32

	resultCh := make(chan BulkActionSummary, 1)
	go func() {
		resultCh <- RunBulkAction(ctx, []string{"issue-a", "issue-b", "issue-c"}, 2,
			func(_ context.Context, issueID string) error {
				current := active.Add(1)
				for {
					old := maxActive.Load()
					if current <= old || maxActive.CompareAndSwap(old, current) {
						break
					}
				}
				defer active.Add(-1)

				switch issueID {
				case "issue-a":
					<-releaseA
				case "issue-b":
					bOnce.Do(func() { close(bStarted) })
				case "issue-c":
					cOnce.Do(func() { close(cStarted) })
				}
				return nil
			}, nil)
	}()

	select {
	case <-bStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for issue-b to start")
	}
	select {
	case <-cStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for issue-c to start after issue-b")
	}
	close(releaseA)

	select {
	case summary := <-resultCh:
		if got := bulkActionResultIDs(summary.Results); !bulkActionStringsEqual(got, []string{"issue-a", "issue-b", "issue-c"}) {
			t.Fatalf("result IDs = %#v, want input order", got)
		}
		if summary.Succeeded != 3 || summary.Failed != 0 {
			t.Fatalf("summary counts = %d succeeded, %d failed; want 3, 0", summary.Succeeded, summary.Failed)
		}
		if got := maxActive.Load(); got > 2 {
			t.Fatalf("maximum concurrent mutations = %d, want at most 2", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bulk action")
	}
}

func TestRunBulkActionRecordsPartialFailuresAndFailedResults(t *testing.T) {
	wantErrA := errors.New("issue-a failed")
	wantErrC := errors.New("issue-c failed")
	summary := RunBulkAction(context.Background(), []string{"issue-a", "issue-b", "issue-c"}, 3,
		func(_ context.Context, issueID string) error {
			switch issueID {
			case "issue-a":
				return wantErrA
			case "issue-c":
				return wantErrC
			default:
				return nil
			}
		}, nil)

	if summary.Succeeded != 1 || summary.Failed != 2 {
		t.Fatalf("summary counts = %d succeeded, %d failed; want 1, 2", summary.Succeeded, summary.Failed)
	}
	if len(summary.Results) != 3 {
		t.Fatalf("results length = %d, want 3", len(summary.Results))
	}
	if !errors.Is(summary.Results[0].Err, wantErrA) || summary.Results[1].Err != nil || !errors.Is(summary.Results[2].Err, wantErrC) {
		t.Fatalf("results errors = %#v, want issue-a/issue-c failures", summary.Results)
	}
	failed := summary.FailedResults()
	if got := bulkActionResultIDs(failed); !bulkActionStringsEqual(got, []string{"issue-a", "issue-c"}) {
		t.Fatalf("failed result IDs = %#v, want issue-a and issue-c order", got)
	}
	failed[0].IssueID = "mutated-copy"
	if summary.Results[0].IssueID != "issue-a" {
		t.Fatal("FailedResults should return a copy")
	}
}

func TestRunBulkActionCancellationCompletesRemainingTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	var progressMu sync.Mutex
	var progress []BulkActionProgress

	summary := RunBulkAction(ctx, []string{"issue-a", "issue-b", "issue-c"}, 1,
		func(_ context.Context, issueID string) error {
			calls.Add(1)
			if issueID != "issue-a" {
				t.Errorf("mutation started for %q after cancellation", issueID)
			}
			cancel()
			return nil
		},
		func(update BulkActionProgress) {
			progressMu.Lock()
			progress = append(progress, update)
			progressMu.Unlock()
		},
	)

	if calls.Load() != 1 {
		t.Fatalf("mutation calls = %d, want 1", calls.Load())
	}
	if len(summary.Results) != 3 || summary.Succeeded != 1 || summary.Failed != 2 {
		t.Fatalf("summary = %#v, want one success and two cancellation failures", summary)
	}
	for i, result := range summary.Results {
		if result.IssueID != []string{"issue-a", "issue-b", "issue-c"}[i] {
			t.Errorf("result[%d] ID = %q, want input order", i, result.IssueID)
		}
		if i > 0 && !errors.Is(result.Err, context.Canceled) {
			t.Errorf("result[%d] error = %v, want context.Canceled", i, result.Err)
		}
	}

	progressMu.Lock()
	defer progressMu.Unlock()
	if len(progress) != 3 || progress[len(progress)-1].Completed != 3 {
		t.Fatalf("progress = %#v, want one update per target through completed=3", progress)
	}
}

func TestRunBulkActionAlreadyCancelledDoesNotMutate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32

	summary := RunBulkAction(ctx, []string{"issue-a", "issue-b"}, 4,
		func(context.Context, string) error {
			calls.Add(1)
			return nil
		}, nil)

	if calls.Load() != 0 {
		t.Fatalf("mutation calls = %d, want 0", calls.Load())
	}
	if summary.Succeeded != 0 || summary.Failed != 2 {
		t.Fatalf("summary counts = %d succeeded, %d failed; want 0, 2", summary.Succeeded, summary.Failed)
	}
	for _, result := range summary.Results {
		if !errors.Is(result.Err, context.Canceled) {
			t.Errorf("result for %s error = %v, want context.Canceled", result.IssueID, result.Err)
		}
	}
}

func TestRunBulkActionNilMutatorAndCallback(t *testing.T) {
	summary := RunBulkAction(context.Background(), []string{"issue-a", "issue-b"}, 0, nil, nil)
	if summary.Succeeded != 0 || summary.Failed != 2 {
		t.Fatalf("summary counts = %d succeeded, %d failed; want 0, 2", summary.Succeeded, summary.Failed)
	}
	for _, result := range summary.Results {
		if result.Err == nil || result.Err.Error() != "bulk action mutate function is nil" {
			t.Errorf("result for %s error = %v, want clear nil-mutator error", result.IssueID, result.Err)
		}
	}
}

func TestRunBulkActionProgressCallbackIsSerialized(t *testing.T) {
	var callbackActive, maxCallbackActive atomic.Int32
	var callbackCount atomic.Int32

	summary := RunBulkAction(context.Background(), []string{"a", "b", "c", "d", "e", "f"}, 6,
		func(context.Context, string) error {
			time.Sleep(time.Millisecond)
			return nil
		},
		func(update BulkActionProgress) {
			current := callbackActive.Add(1)
			for {
				old := maxCallbackActive.Load()
				if current <= old || maxCallbackActive.CompareAndSwap(old, current) {
					break
				}
			}
			if update.Completed <= 0 || update.Completed > update.Total {
				panic(fmt.Sprintf("invalid progress: %#v", update))
			}
			time.Sleep(time.Millisecond)
			callbackCount.Add(1)
			callbackActive.Add(-1)
		},
	)

	if summary.Succeeded != 6 || summary.Failed != 0 {
		t.Fatalf("summary counts = %d succeeded, %d failed; want 6, 0", summary.Succeeded, summary.Failed)
	}
	if callbackCount.Load() != 6 {
		t.Fatalf("callback count = %d, want 6", callbackCount.Load())
	}
	if maxCallbackActive.Load() != 1 {
		t.Fatalf("maximum concurrent callbacks = %d, want 1", maxCallbackActive.Load())
	}
}

func TestRunBulkActionProgressCallbackCanReenterWithoutDeadlock(t *testing.T) {
	var entered atomic.Bool
	callback := func(update BulkActionProgress) {
		if update.Completed != 1 || !entered.CompareAndSwap(false, true) {
			return
		}
		nestedDone := make(chan BulkActionSummary, 1)
		go func() {
			nestedDone <- RunBulkAction(context.Background(), []string{"nested"}, 1,
				func(context.Context, string) error { return nil }, nil)
		}()
		select {
		case nested := <-nestedDone:
			if nested.Succeeded != 1 || nested.Failed != 0 {
				t.Errorf("nested summary = %#v, want one success", nested)
			}
		case <-time.After(time.Second):
			t.Fatal("progress callback re-entry deadlocked")
		}
	}

	done := make(chan BulkActionSummary, 1)
	go func() {
		done <- RunBulkAction(context.Background(), []string{"outer-a", "outer-b"}, 2,
			func(context.Context, string) error { return nil }, callback)
	}()
	select {
	case summary := <-done:
		if summary.Succeeded != 2 || summary.Failed != 0 {
			t.Fatalf("outer summary = %#v, want two successes", summary)
		}
	case <-time.After(time.Second):
		t.Fatal("bulk action did not complete after callback re-entry")
	}
}

func bulkActionResultIDs(results []BulkActionResult) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.IssueID
	}
	return ids
}

func bulkActionStringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
