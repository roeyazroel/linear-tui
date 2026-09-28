package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// TriageAction identifies a schema-backed triage composition.
type TriageAction string

const (
	TriageActionAccept    TriageAction = "accept"
	TriageActionDuplicate TriageAction = "duplicate"
	TriageActionDecline   TriageAction = "decline"
	TriageActionSnooze    TriageAction = "snooze"
)

// ErrTriageDeclineNotConfirmed prevents an accidental archive.
var ErrTriageDeclineNotConfirmed = errors.New("triage decline requires explicit confirmation")

// TriageOperations is the narrow host boundary for schema-backed operations.
// The controller never reaches into App or Client; callers inject methods that
// own context, network access, and any UI scheduling.
type TriageOperations struct {
	UpdateIssue         func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error)
	CreateIssueRelation func(context.Context, linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error)
	ArchiveIssue        func(context.Context, string) error
	SnoozeNotification  func(context.Context, string, time.Time) (linearapi.Notification, error)
}

// TriageActions composes the existing Linear operations into truthful triage
// actions. No dedicated triage mutation is assumed.
type TriageActions struct {
	operations TriageOperations
}

// NewTriageActions creates a controller with explicit injected operations.
func NewTriageActions(operations TriageOperations) *TriageActions {
	return &TriageActions{operations: operations}
}

// TriageAcceptInput chooses the destination workflow state for acceptance.
// DestinationStateType must be a non-triage state type (for example started).
type TriageAcceptInput struct {
	IssueID              string
	DestinationStateID   string
	DestinationStateType string
}

// TriageSnoozeInput identifies the notification associated with a triage item.
type TriageSnoozeInput struct {
	IssueID        string
	NotificationID string
	Until          time.Time
}

// Accept accepts an issue by issuing exactly one existing issueUpdate to the
// caller-selected non-triage workflow state.
func (ta *TriageActions) Accept(ctx context.Context, input TriageAcceptInput) (linearapi.Issue, error) {
	if err := validateTriageAcceptInput(input); err != nil {
		return linearapi.Issue{}, err
	}
	if ta == nil || ta.operations.UpdateIssue == nil {
		return linearapi.Issue{}, errors.New("triage accept: update issue operation unavailable")
	}
	stateID := input.DestinationStateID
	issue, err := ta.operations.UpdateIssue(ctx, linearapi.UpdateIssueInput{ID: input.IssueID, StateID: &stateID})
	if err != nil {
		return linearapi.Issue{}, fmt.Errorf("triage accept %s: %w", input.IssueID, err)
	}
	return issue, nil
}

// AcceptIssue is a convenience wrapper for string-oriented callers.
func (ta *TriageActions) AcceptIssue(ctx context.Context, issueID, destinationStateID, destinationStateType string) (linearapi.Issue, error) {
	return ta.Accept(ctx, TriageAcceptInput{IssueID: issueID, DestinationStateID: destinationStateID, DestinationStateType: destinationStateType})
}

// Duplicate creates exactly one duplicate relation from source issueID to the
// target issue. Linear therefore represents the source as duplicate of target.
func (ta *TriageActions) Duplicate(ctx context.Context, issueID, targetIssueID string) (linearapi.IssueRelation, error) {
	if err := validateTriageID("issue ID", issueID); err != nil {
		return linearapi.IssueRelation{}, err
	}
	if err := validateTriageID("duplicate target issue ID", targetIssueID); err != nil {
		return linearapi.IssueRelation{}, err
	}
	if ta == nil || ta.operations.CreateIssueRelation == nil {
		return linearapi.IssueRelation{}, errors.New("triage duplicate: create relation operation unavailable")
	}
	relation, err := ta.operations.CreateIssueRelation(ctx, linearapi.CreateIssueRelationInput{
		IssueID:        issueID,
		RelatedIssueID: targetIssueID,
		Type:           linearapi.IssueRelationDuplicate,
	})
	if err != nil {
		return linearapi.IssueRelation{}, fmt.Errorf("triage duplicate %s -> %s: %w", issueID, targetIssueID, err)
	}
	return relation, nil
}

// Decline archives an issue only when confirmed is true.
func (ta *TriageActions) Decline(ctx context.Context, issueID string, confirmed bool) error {
	if err := validateTriageID("issue ID", issueID); err != nil {
		return err
	}
	if !confirmed {
		return ErrTriageDeclineNotConfirmed
	}
	if ta == nil || ta.operations.ArchiveIssue == nil {
		return errors.New("triage decline: archive issue operation unavailable")
	}
	if err := ta.operations.ArchiveIssue(ctx, issueID); err != nil {
		return fmt.Errorf("triage decline %s: %w", issueID, err)
	}
	return nil
}

// Snooze updates exactly one related notification. A missing notification ID
// is an explicit error because there is no schema-native issue snooze action.
func (ta *TriageActions) Snooze(ctx context.Context, input TriageSnoozeInput) (linearapi.Notification, error) {
	if err := validateTriageID("issue ID", input.IssueID); err != nil {
		return linearapi.Notification{}, err
	}
	if err := validateTriageID("notification ID", input.NotificationID); err != nil {
		return linearapi.Notification{}, fmt.Errorf("triage snooze: %w", err)
	}
	if input.Until.IsZero() {
		return linearapi.Notification{}, errors.New("triage snooze: snooze time must not be zero")
	}
	if ta == nil || ta.operations.SnoozeNotification == nil {
		return linearapi.Notification{}, errors.New("triage snooze: notification operation unavailable")
	}
	notification, err := ta.operations.SnoozeNotification(ctx, input.NotificationID, input.Until)
	if err != nil {
		return linearapi.Notification{}, fmt.Errorf("triage snooze %s: %w", input.IssueID, err)
	}
	return notification, nil
}

// TriageActionResult records one batch target and its isolated failure.
type TriageActionResult struct {
	Action  TriageAction
	IssueID string
	Err     error
}

// TriageActionProgress is emitted once per normalized target by RunBatch.
type TriageActionProgress struct {
	Action    TriageAction
	Completed int
	Total     int
	Succeeded int
	Failed    int
	Result    TriageActionResult
}

// TriageActionSummary retains all target results so failed IDs can be retried.
type TriageActionSummary struct {
	Action    TriageAction
	Results   []TriageActionResult
	Succeeded int
	Failed    int
	Err       error
}

// FailedResults returns an independent list of failed target results.
func (s TriageActionSummary) FailedResults() []TriageActionResult {
	if len(s.Results) == 0 {
		return nil
	}
	failed := make([]TriageActionResult, 0, s.Failed)
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

// TriageBatchInput describes one bounded batch composition.
type TriageBatchInput struct {
	Action               TriageAction
	IssueIDs             []string
	DestinationStateID   string
	DestinationStateType string
	DuplicateTargetID    string
	Confirmed            bool
	NotificationIDs      map[string]string
	SnoozeUntil          time.Time
	Concurrency          int
}

// RunBatch composes each target through RunBulkAction, preserving bounded
// concurrency, partial failures, normalized order, and one serialized progress
// stream. NotificationIDs maps issue IDs to their related notification IDs for
// the snooze action.
func (ta *TriageActions) RunBatch(ctx context.Context, input TriageBatchInput, onProgress func(TriageActionProgress)) TriageActionSummary {
	summary := TriageActionSummary{Action: input.Action}
	ids, err := normalizeAndValidateTriageIDs(input.IssueIDs)
	if err != nil {
		summary.Err = err
		return summary
	}
	if err := validateTriageBatchInput(input); err != nil {
		summary.Err = err
		return summary
	}
	if len(ids) == 0 {
		return summary
	}
	bulk := RunBulkAction(ctx, ids, input.Concurrency, func(ctx context.Context, issueID string) error {
		switch input.Action {
		case TriageActionAccept:
			_, err := ta.Accept(ctx, TriageAcceptInput{IssueID: issueID, DestinationStateID: input.DestinationStateID, DestinationStateType: input.DestinationStateType})
			return err
		case TriageActionDuplicate:
			_, err := ta.Duplicate(ctx, issueID, input.DuplicateTargetID)
			return err
		case TriageActionDecline:
			return ta.Decline(ctx, issueID, input.Confirmed)
		case TriageActionSnooze:
			_, err := ta.Snooze(ctx, TriageSnoozeInput{IssueID: issueID, NotificationID: input.NotificationIDs[issueID], Until: input.SnoozeUntil})
			return err
		default:
			return fmt.Errorf("unsupported triage action %q", input.Action)
		}
	}, func(update BulkActionProgress) {
		if onProgress != nil {
			onProgress(TriageActionProgress{
				Action:    input.Action,
				Completed: update.Completed,
				Total:     update.Total,
				Succeeded: update.Succeeded,
				Failed:    update.Failed,
				Result: TriageActionResult{
					Action:  input.Action,
					IssueID: update.Result.IssueID,
					Err:     update.Result.Err,
				},
			})
		}
	})
	summary.Results = make([]TriageActionResult, len(bulk.Results))
	for i, result := range bulk.Results {
		summary.Results[i] = TriageActionResult{Action: input.Action, IssueID: result.IssueID, Err: result.Err}
	}
	summary.Succeeded = bulk.Succeeded
	summary.Failed = bulk.Failed
	return summary
}

func validateTriageAcceptInput(input TriageAcceptInput) error {
	if err := validateTriageID("issue ID", input.IssueID); err != nil {
		return err
	}
	if err := validateTriageID("destination state ID", input.DestinationStateID); err != nil {
		return err
	}
	stateType := strings.ToLower(strings.TrimSpace(input.DestinationStateType))
	if stateType == "" {
		return errors.New("triage accept: destination state type is required")
	}
	if stateType == "triage" {
		return errors.New("triage accept: destination state must not be triage")
	}
	return nil
}

func validateTriageBatchInput(input TriageBatchInput) error {
	switch input.Action {
	case TriageActionAccept:
		return validateTriageAcceptInput(TriageAcceptInput{IssueID: "batch-target", DestinationStateID: input.DestinationStateID, DestinationStateType: input.DestinationStateType})
	case TriageActionDuplicate:
		return validateTriageID("duplicate target issue ID", input.DuplicateTargetID)
	case TriageActionDecline:
		if !input.Confirmed {
			return ErrTriageDeclineNotConfirmed
		}
		return nil
	case TriageActionSnooze:
		if input.SnoozeUntil.IsZero() {
			return errors.New("triage snooze: snooze time must not be zero")
		}
		return nil
	default:
		return fmt.Errorf("unsupported triage action %q", input.Action)
	}
}

func normalizeAndValidateTriageIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, errors.New("triage batch: issue IDs must not be empty")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized, nil
}

func validateTriageID(label, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("triage: %s must not be empty", label)
	}
	return nil
}
