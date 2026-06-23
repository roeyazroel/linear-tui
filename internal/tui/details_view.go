package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// defaultMarkdownWidth is used when a target view's width is not yet known.
const defaultMarkdownWidth = 80

// minMarkdownWidth guards against rendering into an unusably narrow column.
const minMarkdownWidth = 20

// markdownRenderers caches one glamour renderer per word-wrap width. Renderers
// are relatively expensive to construct, and the details panes only re-render
// on selection, so caching by width avoids rebuilding on every update.
var markdownRenderers = make(map[int]*glamour.TermRenderer)

func uintPtr(v uint) *uint { return &v }

// markdownRendererForWidth returns a renderer that word-wraps at the given
// width. It uses the dark style but with the document margin removed so that
// rendered lines begin at column 0 — the surrounding TextView already supplies
// its own border padding, and the default 2-column margin caused every line
// (including wrapped continuations) to be indented inconsistently.
func markdownRendererForWidth(width int) *glamour.TermRenderer {
	if width < minMarkdownWidth {
		width = minMarkdownWidth
	}
	if r, ok := markdownRenderers[width]; ok {
		return r
	}

	style := styles.DarkStyleConfig
	style.Document.Margin = uintPtr(0)

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		// Fallback: a basic auto-styled renderer if the custom style fails.
		r, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
	}
	markdownRenderers[width] = r
	return r
}

// renderMarkdownWidth renders markdown content, word-wrapped to width columns.
// Falls back to the raw content if rendering fails.
func renderMarkdownWidth(content string, width int) string {
	rendered, err := markdownRendererForWidth(width).Render(content)
	if err != nil {
		// Fallback to plain text on error
		return content
	}

	// Trim extra whitespace that glamour may add
	return strings.TrimSpace(rendered)
}

// renderMarkdown renders markdown at a default width, for callers without a
// concrete target width.
func renderMarkdown(content string) string {
	return renderMarkdownWidth(content, defaultMarkdownWidth)
}

// textViewInnerWidth returns the usable text width of a TextView (excluding its
// border and padding), or defaultMarkdownWidth if the view has not been laid
// out yet.
func textViewInnerWidth(tv *tview.TextView) int {
	if tv == nil {
		return defaultMarkdownWidth
	}
	_, _, w, _ := tv.GetInnerRect()
	if w <= 0 {
		return defaultMarkdownWidth
	}
	return w
}

func formatIssueReference(ref linearapi.IssueRef) string {
	if ref.Identifier == "" {
		return ref.ID
	}
	if ref.Title == "" {
		return ref.Identifier
	}
	return fmt.Sprintf("%s - %s", ref.Identifier, ref.Title)
}

func formatUserDisplayName(user linearapi.User) string {
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Name != "" {
		return user.Name
	}
	return user.ID
}

// buildDetailsView creates and configures the details view with separate description and comments sections.
func (a *App) buildDetailsView() *tview.Flex {
	// Create description/metadata view (top section, scrollable)
	a.detailsDescriptionView = tview.NewTextView()
	a.detailsDescriptionView.SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" Details ").
		SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(tcell.ColorDefault)
	padding := a.density.DetailsPadding
	a.detailsDescriptionView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	// Create comments view (bottom section, scrollable, fixed height)
	a.detailsCommentsView = tview.NewTextView()
	a.detailsCommentsView.SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetBorder(true).
		SetTitle(" Comments ").
		SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(tcell.ColorDefault)
	a.detailsCommentsView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	// Create flex layout; comments are added conditionally after issue selection.
	detailsFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	a.detailsView = detailsFlex
	a.setDetailsCommentsVisibility(false)

	return a.detailsView
}

// setDetailsCommentsVisibility rebuilds the details layout to show or hide comments.
func (a *App) setDetailsCommentsVisibility(showComments bool) {
	if a.detailsView == nil || a.detailsDescriptionView == nil || a.detailsCommentsView == nil {
		return
	}
	if a.detailsCommentsVisible == showComments && a.detailsView.GetItemCount() > 0 {
		return
	}

	a.detailsView.Clear().
		AddItem(a.detailsDescriptionView, 0, 3, true)
	if showComments {
		a.detailsView.AddItem(a.detailsCommentsView, 0, 2, false)
	}

	a.detailsCommentsVisible = showComments
	if !showComments {
		a.focusedDetailsView = false
	}
}

// updateDetailsView updates the details view with the selected issue.
func (a *App) updateDetailsView() {
	a.issuesMu.RLock()
	selectedIssue := a.selectedIssue
	a.issuesMu.RUnlock()
	hasComments := selectedIssue != nil && len(selectedIssue.Comments) > 0
	a.setDetailsCommentsVisibility(hasComments)
	if selectedIssue == nil {
		a.detailsDescriptionView.SetText(fmt.Sprintf("%sNo issue selected. Select an issue from the list to view details.[-]", a.themeTags.SecondaryText))
		a.detailsCommentsView.SetText("")
		if a.focusedPane == FocusDetails && !a.detailsCommentsVisible {
			a.updateFocus()
		}
		return
	}

	issue := selectedIssue

	// Helper to colorize keys
	keyColor := a.themeTags.SecondaryText
	valColor := a.themeTags.Foreground
	accentColor := a.themeTags.Accent
	dividerColor := a.themeTags.Border
	sectionGap := a.density.DetailsSectionGap

	// ===== Update Description/Metadata View =====
	var headerLines []string

	// Issue header info with styling
	headerLines = append(headerLines, fmt.Sprintf("%s%s[-]", accentColor, issue.Identifier))
	headerLines = append(headerLines, fmt.Sprintf("[b]%s%s[-]", valColor, issue.Title))
	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}

	// Metadata grid simulation
	stateColorTag := stateToColorTag(issue.State, a.theme)
	headerLines = append(headerLines, fmt.Sprintf("%sState:[-]      %s%s[-]", keyColor, stateColorTag, issue.State))

	assignee := "Unassigned"
	if issue.Assignee != "" {
		assignee = issue.Assignee
	}
	headerLines = append(headerLines, fmt.Sprintf("%sAssignee:[-]   %s%s[-]", keyColor, valColor, assignee))

	priorityLabel, priorityColor, _ := formatPriority(issue.Priority, a.theme)
	priorityTag := colorTag(priorityColor)
	headerLines = append(headerLines, fmt.Sprintf("%sPriority:[-]   %s%s[-]", keyColor, priorityTag, priorityLabel))

	cycle := "No cycle"
	if issue.Cycle != nil {
		cycle = issue.Cycle.DisplayName()
	}
	headerLines = append(headerLines, fmt.Sprintf("%sCycle:[-]      %s%s[-]", keyColor, valColor, cycle))

	headerLines = append(headerLines, fmt.Sprintf("%sDue date:[-]   %s%s[-]", keyColor, valColor, formatDueDate(issue.DueDate)))
	headerLines = append(headerLines, fmt.Sprintf("%sEstimate:[-]   %s%s[-]", keyColor, valColor, formatEstimate(issue.Estimate)))
	headerLines = append(headerLines, fmt.Sprintf("%sMilestone:[-]  %s%s[-]", keyColor, valColor, formatMilestoneName(issue.ProjectMilestone)))

	// Labels
	labelsText := "No labels"
	if len(issue.Labels) > 0 {
		labelNames := make([]string, len(issue.Labels))
		for i, lbl := range issue.Labels {
			labelNames[i] = lbl.Name
		}
		labelsText = strings.Join(labelNames, ", ")
	}
	headerLines = append(headerLines, fmt.Sprintf("%sLabels:[-]     %s%s[-]", keyColor, valColor, labelsText))

	// Parent issue (if this is a sub-issue)
	if issue.Parent != nil {
		parentText := fmt.Sprintf("%s - %s", issue.Parent.Identifier, issue.Parent.Title)
		headerLines = append(headerLines, fmt.Sprintf("%sParent:[-]     %s%s[-]", keyColor, accentColor, parentText))
	}

	// Sub-issues (if this is a parent issue)
	if len(issue.Children) > 0 {
		for i := 0; i < sectionGap; i++ {
			headerLines = append(headerLines, "")
		}
		headerLines = append(headerLines, fmt.Sprintf("%sSub-issues:[-] %s%d items[-]", keyColor, valColor, len(issue.Children)))
		for _, child := range issue.Children {
			// Show child identifier, state, and title
			childStateTag := stateToColorTag(child.State, a.theme)
			childLine := fmt.Sprintf("  %s└─[-] %s%s[-] %s%s[-] %s%s[-]",
				keyColor,
				accentColor, child.Identifier,
				childStateTag, child.State,
				valColor, child.Title)
			headerLines = append(headerLines, childLine)
		}
	}

	if len(issue.Relations) > 0 {
		for i := 0; i < sectionGap; i++ {
			headerLines = append(headerLines, "")
		}
		headerLines = append(headerLines, fmt.Sprintf("%sRelations:[-] %s%d items[-]", keyColor, valColor, len(issue.Relations)))
		for _, relation := range issue.Relations {
			ref := relation.RelatedIssue
			if relation.Inverse {
				ref = relation.Issue
			}
			headerLines = append(headerLines, fmt.Sprintf("  %s%s[-] %s%s[-]", keyColor, relation.DisplayType(), accentColor, formatIssueReference(ref)))
		}
	}

	if len(issue.Subscribers) > 0 {
		for i := 0; i < sectionGap; i++ {
			headerLines = append(headerLines, "")
		}
		subscribers := make([]string, 0, len(issue.Subscribers))
		for _, subscriber := range issue.Subscribers {
			subscribers = append(subscribers, formatUserDisplayName(subscriber))
		}
		headerLines = append(headerLines, fmt.Sprintf("%sSubscribers:[-] %s%s[-]", keyColor, valColor, strings.Join(subscribers, ", ")))
	}

	if len(issue.Attachments) > 0 {
		for i := 0; i < sectionGap; i++ {
			headerLines = append(headerLines, "")
		}
		headerLines = append(headerLines, fmt.Sprintf("%sAttachments:[-] %s%d items[-]", keyColor, valColor, len(issue.Attachments)))
		for _, attachment := range issue.Attachments {
			title := attachment.Title
			if title == "" {
				title = attachment.URL
			}
			source := attachment.SourceType
			if source != "" {
				source = " (" + source + ")"
			}
			headerLines = append(headerLines, fmt.Sprintf("  %s%s%s[-] %s%s[-]", accentColor, title, source, keyColor, attachment.URL))
		}
	}

	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}
	headerLines = append(headerLines, fmt.Sprintf("%s────────────────────────────────────────[-]", dividerColor))
	for i := 0; i < sectionGap; i++ {
		headerLines = append(headerLines, "")
	}

	// Set header first, then append description via ANSIWriter
	a.detailsDescriptionView.Clear()
	a.detailsDescriptionView.SetText(strings.Join(headerLines, "\n"))
	writer := tview.ANSIWriter(a.detailsDescriptionView)

	// Description
	if issue.Description != "" {
		_, _ = fmt.Fprintf(writer, "%sDescription:[-]\n\n", keyColor)

		// Render description as markdown and write through ANSIWriter.
		// ANSIWriter translates ANSI escape codes to tview color tags.
		// Wrap to the pane's actual width so glamour does not pre-wrap wider
		// than the column (which left tview to re-wrap into ragged lines).
		renderedDesc := renderMarkdownWidth(issue.Description, textViewInnerWidth(a.detailsDescriptionView))
		_, _ = fmt.Fprint(writer, renderedDesc)
	} else {
		_, _ = fmt.Fprintf(writer, "%sNo description available[-]", keyColor)
	}

	a.detailsDescriptionView.ScrollToBeginning()

	// ===== Update Comments View =====
	a.detailsCommentsView.Clear()
	commentsWriter := tview.ANSIWriter(a.detailsCommentsView)

	if len(issue.Comments) > 0 {
		_, _ = fmt.Fprintf(commentsWriter, "%sComments:[-] (%d)\n\n", keyColor, len(issue.Comments))

		for i, comment := range issue.Comments {
			// Comment header: author and timestamp
			authorDisplay := comment.Author.DisplayName
			if authorDisplay == "" {
				authorDisplay = comment.Author.Name
			}
			if comment.Author.IsMe {
				authorDisplay = fmt.Sprintf("%s (me)", authorDisplay)
			}

			// Format timestamp
			timeStr := comment.CreatedAt.Format("Jan 2, 2006 3:04 PM")
			if !comment.UpdatedAt.Equal(comment.CreatedAt) {
				timeStr += " (edited)"
			}

			_, _ = fmt.Fprintf(commentsWriter, "%s%s[-] %s%s[-]\n", accentColor, authorDisplay, keyColor, timeStr)
			_, _ = fmt.Fprint(commentsWriter, "\n")

			// Render comment body as markdown, wrapped to the comments pane width.
			renderedComment := renderMarkdownWidth(comment.Body, textViewInnerWidth(a.detailsCommentsView))
			_, _ = fmt.Fprint(commentsWriter, renderedComment)

			// Add separator between comments (but not after the last one)
			if i < len(issue.Comments)-1 {
				_, _ = fmt.Fprint(commentsWriter, "\n\n")
				_, _ = fmt.Fprintf(commentsWriter, "%s────────────────────────────────────────[-]\n\n", dividerColor)
			}
		}
	} else {
		// Empty state for comments
		_, _ = fmt.Fprintf(commentsWriter, "%sNo comments yet.[-]", keyColor)
	}

	a.detailsCommentsView.ScrollToBeginning()
	if a.focusedPane == FocusDetails && !a.detailsCommentsVisible {
		a.updateFocus()
	}
}

// stateToColorTag maps a Linear workflow state name to a tview color tag using
// the active theme. It uses the same matching logic as the table renderer.
func stateToColorTag(state string, theme Theme) string {
	lower := strings.ToLower(state)
	var c tcell.Color
	switch {
	case strings.Contains(lower, "done") || strings.Contains(lower, "complete"):
		c = theme.StatusDone
	case strings.Contains(lower, "cancel"):
		c = theme.StatusCanceled
	case strings.Contains(lower, "progress"):
		c = theme.StatusInProgress
	case strings.Contains(lower, "review"):
		c = theme.StatusInReview
	case strings.Contains(lower, "triage"):
		c = theme.StatusTriage
	case strings.Contains(lower, "backlog"):
		c = theme.StatusBacklog
	default:
		c = theme.StatusTodo
	}
	return colorTag(c)
}
