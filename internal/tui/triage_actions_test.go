package tui

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestTriageAcceptUpdatesExactlyOnceToNonTriageState(t *testing.T) {
	var calls []linearapi.UpdateIssueInput
	controller := NewTriageActions(TriageOperations{
		UpdateIssue: func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
			calls = append(calls, input)
			return linearapi.Issue{ID: input.ID, StateID: *input.StateID}, nil
		},
	})

	issue, err := controller.Accept(context.Background(), TriageAcceptInput{
		IssueID:              "issue-1",
		DestinationStateID:   "state-started",
		DestinationStateType: "started",
	})
	if err != nil {
		t.Fatalf("Accept() error: %v", err)
	}
	if issue.ID != "issue-1" || issue.StateID != "state-started" || len(calls) != 1 {
		t.Fatalf("accept result/calls = (%+v, %d)", issue, len(calls))
	}
	if calls[0].ID != "issue-1" || calls[0].StateID == nil || *calls[0].StateID != "state-started" {
		t.Fatalf("update input = %+v", calls[0])
	}
}

func TestTriageAcceptRejectsTriageDestinationWithoutNetwork(t *testing.T) {
	calls := 0
	controller := NewTriageActions(TriageOperations{UpdateIssue: func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		calls++
		return linearapi.Issue{}, nil
	}})
	_, err := controller.Accept(context.Background(), TriageAcceptInput{IssueID: "issue-1", DestinationStateID: "state-triage", DestinationStateType: "triage"})
	if err == nil {
		t.Fatal("Accept() error = nil, want triage-state validation error")
	}
	if calls != 0 {
		t.Fatalf("update calls = %d, want 0", calls)
	}
}

func TestTriageDuplicateUsesSourceToTargetDirection(t *testing.T) {
	var got linearapi.CreateIssueRelationInput
	controller := NewTriageActions(TriageOperations{
		CreateIssueRelation: func(_ context.Context, input linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error) {
			got = input
			return linearapi.IssueRelation{ID: "relation-1"}, nil
		},
	})
	if _, err := controller.Duplicate(context.Background(), "issue-source", "issue-target"); err != nil {
		t.Fatalf("Duplicate() error: %v", err)
	}
	want := linearapi.CreateIssueRelationInput{IssueID: "issue-source", RelatedIssueID: "issue-target", Type: linearapi.IssueRelationDuplicate}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relation input = %+v, want %+v", got, want)
	}
}

func TestTriageDeclineRequiresExplicitConfirmation(t *testing.T) {
	calls := 0
	controller := NewTriageActions(TriageOperations{ArchiveIssue: func(context.Context, string) error {
		calls++
		return nil
	}})
	if err := controller.Decline(context.Background(), "issue-1", false); !errors.Is(err, ErrTriageDeclineNotConfirmed) {
		t.Fatalf("unconfirmed decline error = %v, want ErrTriageDeclineNotConfirmed", err)
	}
	if calls != 0 {
		t.Fatalf("archive calls after unconfirmed decline = %d, want 0", calls)
	}
	if err := controller.Decline(context.Background(), "issue-1", true); err != nil {
		t.Fatalf("confirmed decline error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("archive calls after confirmed decline = %d, want 1", calls)
	}
}

func TestTriageSnoozeRequiresNotificationIDAndCallsOnce(t *testing.T) {
	calls := 0
	until := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	controller := NewTriageActions(TriageOperations{SnoozeNotification: func(_ context.Context, notificationID string, gotUntil time.Time) (linearapi.Notification, error) {
		calls++
		if notificationID != "notification-1" || !gotUntil.Equal(until) {
			t.Errorf("snooze args = (%q, %v)", notificationID, gotUntil)
		}
		return linearapi.Notification{ID: notificationID}, nil
	}})
	if _, err := controller.Snooze(context.Background(), TriageSnoozeInput{IssueID: "issue-1", NotificationID: "", Until: until}); err == nil {
		t.Fatal("Snooze() without notification ID error = nil")
	}
	if calls != 0 {
		t.Fatalf("calls after missing notification ID = %d, want 0", calls)
	}
	if _, err := controller.Snooze(context.Background(), TriageSnoozeInput{IssueID: "issue-1", NotificationID: "notification-1", Until: until}); err != nil {
		t.Fatalf("Snooze() error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("snooze calls = %d, want 1", calls)
	}
}

func TestTriageActionsValidateIDsAndRequiredInputsBeforeNetwork(t *testing.T) {
	var calls int
	controller := NewTriageActions(TriageOperations{
		UpdateIssue: func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
			calls++
			return linearapi.Issue{}, nil
		},
		CreateIssueRelation: func(context.Context, linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error) {
			calls++
			return linearapi.IssueRelation{}, nil
		},
		ArchiveIssue: func(context.Context, string) error { calls++; return nil },
	})
	if _, err := controller.Accept(context.Background(), TriageAcceptInput{IssueID: "", DestinationStateID: "state", DestinationStateType: "started"}); err == nil {
		t.Fatal("empty accept ID error = nil")
	}
	if _, err := controller.Duplicate(context.Background(), "issue-1", " "); err == nil {
		t.Fatal("empty duplicate target error = nil")
	}
	if err := controller.Decline(context.Background(), " ", true); err == nil {
		t.Fatal("empty decline ID error = nil")
	}
	if _, err := controller.Snooze(context.Background(), TriageSnoozeInput{IssueID: "issue-1", NotificationID: "notification-1"}); err == nil {
		t.Fatal("zero snooze timestamp error = nil")
	}
	if calls != 0 {
		t.Fatalf("network operation calls = %d, want 0", calls)
	}
}

func TestTriageRunBatchUsesBoundedProgressAndRetainsFailures(t *testing.T) {
	var mu sync.Mutex
	var active, maxActive int
	progress := make([]TriageActionProgress, 0)
	controller := NewTriageActions(TriageOperations{ArchiveIssue: func(_ context.Context, issueID string) error {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		if issueID == "issue-b" {
			return errors.New("archive failed")
		}
		return nil
	}})
	summary := controller.RunBatch(context.Background(), TriageBatchInput{
		Action:      TriageActionDecline,
		IssueIDs:    []string{"issue-a", "issue-b", "issue-a", "issue-c"},
		Confirmed:   true,
		Concurrency: 2,
	}, func(update TriageActionProgress) {
		mu.Lock()
		progress = append(progress, update)
		mu.Unlock()
	})
	if summary.Succeeded != 2 || summary.Failed != 1 || len(summary.Results) != 3 {
		t.Fatalf("summary = %+v, want 2 succeeded/1 failed/3 results", summary)
	}
	if maxActive > 2 {
		t.Fatalf("max concurrency = %d, want <=2", maxActive)
	}
	if len(progress) != 3 || progress[len(progress)-1].Completed != 3 {
		t.Fatalf("progress = %+v, want one update per normalized target", progress)
	}
	failed := summary.FailedResults()
	if len(failed) != 1 || failed[0].IssueID != "issue-b" {
		t.Fatalf("failed results = %+v, want issue-b retained", failed)
	}
}

func TestTriageBatchValidationAvoidsNetwork(t *testing.T) {
	calls := 0
	controller := NewTriageActions(TriageOperations{ArchiveIssue: func(context.Context, string) error { calls++; return nil }})
	summary := controller.RunBatch(context.Background(), TriageBatchInput{Action: TriageActionDecline, IssueIDs: []string{"issue-1"}, Confirmed: false}, nil)
	if summary.Err == nil || calls != 0 {
		t.Fatalf("summary/calls = (%+v, %d), want validation error and no calls", summary, calls)
	}
}
