package tui

import (
	"testing"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestSplitIssuesByAssignee(t *testing.T) {
	currentUserID := "user-123"

	tests := []struct {
		name           string
		issues         []linearapi.Issue
		currentUserID  string
		wantMyCount    int
		wantOtherCount int
	}{
		{
			name: "no current user - all issues go to other",
			issues: []linearapi.Issue{
				{ID: "1", AssigneeID: "user-123"},
				{ID: "2", AssigneeID: "user-456"},
			},
			currentUserID:  "",
			wantMyCount:    0,
			wantOtherCount: 2,
		},
		{
			name: "mixed assignment - correct partition",
			issues: []linearapi.Issue{
				{ID: "1", AssigneeID: "user-123"},
				{ID: "2", AssigneeID: "user-456"},
				{ID: "3", AssigneeID: "user-123"},
				{ID: "4", AssigneeID: ""},
			},
			currentUserID:  currentUserID,
			wantMyCount:    2,
			wantOtherCount: 2,
		},
		{
			name: "unassigned issues go to other",
			issues: []linearapi.Issue{
				{ID: "1", AssigneeID: ""},
				{ID: "2", AssigneeID: ""},
				{ID: "3", AssigneeID: currentUserID},
			},
			currentUserID:  currentUserID,
			wantMyCount:    1,
			wantOtherCount: 2,
		},
		{
			name: "all my issues",
			issues: []linearapi.Issue{
				{ID: "1", AssigneeID: currentUserID},
				{ID: "2", AssigneeID: currentUserID},
			},
			currentUserID:  currentUserID,
			wantMyCount:    2,
			wantOtherCount: 0,
		},
		{
			name: "all other issues",
			issues: []linearapi.Issue{
				{ID: "1", AssigneeID: "user-456"},
				{ID: "2", AssigneeID: "user-789"},
			},
			currentUserID:  currentUserID,
			wantMyCount:    0,
			wantOtherCount: 2,
		},
		{
			name:           "empty issues list",
			issues:         []linearapi.Issue{},
			currentUserID:  currentUserID,
			wantMyCount:    0,
			wantOtherCount: 0,
		},
		{
			name: "split by own assignee - parent mine, children unassigned",
			issues: []linearapi.Issue{
				{ID: "parent-1", AssigneeID: currentUserID},
				{ID: "child-1", AssigneeID: "", Parent: &linearapi.IssueRef{ID: "parent-1"}},
				{ID: "child-2", AssigneeID: "", Parent: &linearapi.IssueRef{ID: "parent-1"}},
			},
			currentUserID:  currentUserID,
			wantMyCount:    1, // Only the parent is assigned to me
			wantOtherCount: 2, // Unassigned children
		},
		{
			name: "split by own assignee - parent someone else, children assigned to me",
			issues: []linearapi.Issue{
				{ID: "parent-2", AssigneeID: "user-456"},
				{ID: "child-3", AssigneeID: currentUserID, Parent: &linearapi.IssueRef{ID: "parent-2"}},
				{ID: "child-4", AssigneeID: currentUserID, Parent: &linearapi.IssueRef{ID: "parent-2"}},
			},
			currentUserID:  currentUserID,
			wantMyCount:    2, // My sub-issues surface in My Issues
			wantOtherCount: 1, // The parent belongs to someone else
		},
		{
			name: "split by own assignee - nested, only some mine",
			issues: []linearapi.Issue{
				{ID: "parent-3", AssigneeID: currentUserID},
				{ID: "child-5", AssigneeID: "", Parent: &linearapi.IssueRef{ID: "parent-3"}},
				{ID: "grandchild-1", AssigneeID: currentUserID, Parent: &linearapi.IssueRef{ID: "child-5"}},
			},
			currentUserID:  currentUserID,
			wantMyCount:    2, // parent + grandchild (both mine)
			wantOtherCount: 1, // unassigned child
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			my, other := splitIssuesByAssignee(tt.issues, tt.currentUserID)

			if len(my) != tt.wantMyCount {
				t.Errorf("splitIssuesByAssignee() my count = %d, want %d", len(my), tt.wantMyCount)
			}

			if len(other) != tt.wantOtherCount {
				t.Errorf("splitIssuesByAssignee() other count = %d, want %d", len(other), tt.wantOtherCount)
			}

			// Verify correctness: every "my" issue is assigned to the current
			// user, regardless of parent/child relationship.
			for _, issue := range my {
				if issue.AssigneeID != tt.currentUserID {
					t.Errorf("splitIssuesByAssignee() my issue %s has AssigneeID %s, want %s", issue.ID, issue.AssigneeID, tt.currentUserID)
				}
			}

			// Verify correctness: no "other" issue is assigned to the current user
			// (only meaningful when a current user is set).
			if tt.currentUserID != "" {
				for _, issue := range other {
					if issue.AssigneeID == tt.currentUserID {
						t.Errorf("splitIssuesByAssignee() other issue %s is assigned to current user but is in other section", issue.ID)
					}
				}
			}

			// Verify all issues are accounted for
			if len(my)+len(other) != len(tt.issues) {
				t.Errorf("splitIssuesByAssignee() total issues = %d, want %d", len(my)+len(other), len(tt.issues))
			}
		})
	}
}
