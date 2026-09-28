package tui

import (
	"context"
	"errors"
	"sync"
)

// BulkActionResult is the result of applying a bulk action to one issue.
type BulkActionResult struct {
	IssueID string
	Err     error
}

// BulkActionProgress reports the latest completed bulk-action target.
//
// Result is the result represented by this progress update. Progress updates
// are emitted once per normalized target and are serialized by RunBulkAction.
type BulkActionProgress struct {
	Completed int
	Total     int
	Succeeded int
	Failed    int
	Result    BulkActionResult
}

// BulkActionSummary contains all bulk-action results in normalized input
// order, along with aggregate success and failure counts.
type BulkActionSummary struct {
	Results   []BulkActionResult
	Succeeded int
	Failed    int
}

// FailedResults returns failed results in the same order as Summary.Results.
// The returned slice is independent of the summary.
func (s BulkActionSummary) FailedResults() []BulkActionResult {
	if len(s.Results) == 0 {
		return nil
	}

	capacity := s.Failed
	if capacity < 0 {
		capacity = 0
	}
	failed := make([]BulkActionResult, 0, capacity)
	for _, result := range s.Results {
		if result.Err != nil {
			failed = append(failed, result)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

var errNilBulkActionMutate = errors.New("bulk action mutate function is nil")

type bulkActionJob struct {
	index   int
	issueID string
}

// RunBulkAction applies mutate to each non-empty, unique issue ID.
//
// Mutations run concurrently, bounded by concurrency (or four when
// concurrency is non-positive). Results retain normalized input order even
// when mutations complete out of order. A progress update is emitted once for
// every normalized target, including targets skipped because the context was
// canceled. Mutation errors are isolated to their target and do not stop the
// remaining work.
func RunBulkAction(
	ctx context.Context,
	issueIDs []string,
	concurrency int,
	mutate func(context.Context, string) error,
	onProgress func(BulkActionProgress),
) BulkActionSummary {
	if ctx == nil {
		ctx = context.Background()
	}

	normalized := normalizeBulkActionIssueIDs(issueIDs)
	summary := BulkActionSummary{
		Results: make([]BulkActionResult, len(normalized)),
	}
	if len(normalized) == 0 {
		return summary
	}

	if concurrency <= 0 {
		concurrency = 4
	}
	if concurrency > len(normalized) {
		concurrency = len(normalized)
	}

	var progressMu sync.Mutex
	completed := 0
	succeeded := 0
	failed := 0
	// Progress state and callback delivery use separate synchronization. A
	// callback may re-enter caller code or block, so it must never execute
	// while progressMu is held. callbackCond still preserves the sequence in
	// which snapshots were recorded, keeping delivery serialized and stable.
	var callbackMu sync.Mutex
	callbackCond := sync.NewCond(&callbackMu)
	nextCallback := 1
	emitProgress := func(sequence int, update BulkActionProgress) {
		if onProgress == nil {
			return
		}
		callbackMu.Lock()
		for sequence != nextCallback {
			callbackCond.Wait()
		}
		callbackMu.Unlock()
		defer func() {
			callbackMu.Lock()
			nextCallback++
			callbackCond.Broadcast()
			callbackMu.Unlock()
		}()
		onProgress(update)
	}
	record := func(index int, err error) {
		progressMu.Lock()
		result := BulkActionResult{IssueID: normalized[index], Err: err}
		summary.Results[index] = result
		completed++
		if err == nil {
			succeeded++
		} else {
			failed++
		}
		progress := BulkActionProgress{
			Completed: completed,
			Total:     len(normalized),
			Succeeded: succeeded,
			Failed:    failed,
			Result:    result,
		}
		sequence := completed
		progressMu.Unlock()
		// Snapshotting is complete before callback delivery begins. This keeps
		// re-entrant or slow callbacks from blocking state updates.
		emitProgress(sequence, progress)
	}

	if err := ctx.Err(); err != nil {
		for index := range normalized {
			record(index, err)
		}
		summary.Succeeded = succeeded
		summary.Failed = failed
		return summary
	}

	if mutate == nil {
		for index := range normalized {
			err := ctx.Err()
			if err == nil {
				err = errNilBulkActionMutate
			}
			record(index, err)
		}
		summary.Succeeded = succeeded
		summary.Failed = failed
		return summary
	}

	jobs := make(chan bulkActionJob)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for worker := 0; worker < concurrency; worker++ {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					if err := ctx.Err(); err != nil {
						record(job.index, err)
						continue
					}
					record(job.index, mutate(ctx, job.issueID))
				}
			}
		}()
	}

	next := 0
	for next < len(normalized) {
		if err := ctx.Err(); err != nil {
			for ; next < len(normalized); next++ {
				record(next, err)
			}
			break
		}

		select {
		case <-ctx.Done():
			err := ctx.Err()
			for ; next < len(normalized); next++ {
				record(next, err)
			}
		case jobs <- bulkActionJob{index: next, issueID: normalized[next]}:
			next++
		}
	}
	close(jobs)
	workers.Wait()

	// A worker can observe cancellation after the dispatcher has handed it a
	// job. Every such job is recorded by the worker's context check above. The
	// dispatcher records all jobs it never handed to a worker, so every target
	// has exactly one result before this function returns.
	summary.Succeeded = succeeded
	summary.Failed = failed
	return summary
}

func normalizeBulkActionIssueIDs(issueIDs []string) []string {
	if len(issueIDs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(issueIDs))
	normalized := make([]string, 0, len(issueIDs))
	for _, issueID := range issueIDs {
		if issueID == "" {
			continue
		}
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		normalized = append(normalized, issueID)
	}
	return normalized
}
