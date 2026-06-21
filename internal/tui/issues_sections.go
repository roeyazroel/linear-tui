package tui

import "github.com/roeyazroel/linear-tui/internal/linearapi"

// splitIssuesByAssignee partitions issues into "My Issues" and "Other Issues"
// based purely on each issue's own assignee: an issue goes into "my" when its
// AssigneeID matches currentUserID, otherwise into "other". This is independent
// of parent/child relationships, so a sub-issue assigned to the current user
// appears in "My Issues" even when its parent belongs to someone else.
// BuildIssueRows promotes such a sub-issue to a top-level row when its parent is
// absent from the section. If currentUserID is empty, all issues go into "other".
func splitIssuesByAssignee(issues []linearapi.Issue, currentUserID string) (my []linearapi.Issue, other []linearapi.Issue) {
	my = make([]linearapi.Issue, 0)
	other = make([]linearapi.Issue, 0)

	// If no current user, all issues go to "other".
	if currentUserID == "" {
		other = issues
		return my, other
	}

	for i := range issues {
		issue := &issues[i]
		if issue.AssigneeID == currentUserID {
			my = append(my, *issue)
		} else {
			other = append(other, *issue)
		}
	}

	return my, other
}
