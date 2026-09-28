package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// CreateIssueModal manages the create issue form overlay.
type CreateIssueModal struct {
	app           *App
	modal         *tview.Flex
	form          *tview.Form
	headerView    *tview.TextView
	parentView    *tview.TextView
	assigneeField *tview.DropDown
	cycleField    *tview.DropDown
	priorityField *tview.DropDown
	teamID        string
	projectID     string
	assigneeID    string
	assigneeName  string
	cycleID       string
	cycleName     string
	selectedCycle string
	priority      int
	priorityLabel string
	onCreate      func(title, description, teamID, projectID, assigneeID, cycleID string, priority int)
	cachedUsers   []struct{ ID, Name string }
	cachedCycles  []struct{ ID, Name string }
}

type CreateIssueModalOptions struct {
	TeamID    string
	ProjectID string
	Parent    *linearapi.IssueRef
	CycleID   string
}

// NewCreateIssueModal creates a new create issue modal.
func NewCreateIssueModal(app *App) *CreateIssueModal {
	cm := &CreateIssueModal{
		app:      app,
		priority: 3, // Default: Normal
	}

	// Create form
	cm.form = tview.NewForm()
	cm.form.SetBackgroundColor(app.theme.HeaderBg)
	cm.form.SetFieldBackgroundColor(app.theme.InputBg)
	cm.form.SetFieldTextColor(app.theme.Foreground)
	cm.form.SetButtonBackgroundColor(app.theme.Accent)
	cm.form.SetButtonTextColor(app.theme.SelectionText)
	cm.form.SetLabelColor(app.theme.Foreground)

	// Add title field
	cm.form.AddInputField("Title", "", 60, nil, nil)

	// Add description field
	cm.form.AddTextArea("Description", "", 60, 4, 0, nil)

	// Add assignee dropdown - will be populated when shown
	cm.form.AddDropDown("Assignee", []string{"Unassigned"}, 0, func(_ string, index int) {
		if index == 0 {
			cm.assigneeID = ""
			cm.assigneeName = ""
		} else if index > 0 && index <= len(cm.cachedUsers) {
			user := cm.cachedUsers[index-1]
			cm.assigneeID = user.ID
			cm.assigneeName = user.Name
		}
	})
	// Get the dropdown and style it
	if item := cm.form.GetFormItemByLabel("Assignee"); item != nil {
		if dropdown, ok := item.(*tview.DropDown); ok {
			cm.assigneeField = dropdown
		}
	}
	cm.assigneeField.SetFieldWidth(50)
	cm.assigneeField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)

	cm.form.AddDropDown("Cycle", []string{"No cycle"}, 0, func(_ string, index int) {
		if index == 0 {
			cm.cycleID = ""
			cm.cycleName = ""
		} else if index > 0 && index <= len(cm.cachedCycles) {
			cycle := cm.cachedCycles[index-1]
			cm.cycleID = cycle.ID
			cm.cycleName = cycle.Name
		}
	})
	if item := cm.form.GetFormItemByLabel("Cycle"); item != nil {
		if dropdown, ok := item.(*tview.DropDown); ok {
			cm.cycleField = dropdown
		}
	}
	cm.cycleField.SetFieldWidth(50)
	cm.cycleField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)

	// Add priority dropdown with all options
	priorities := []string{"No priority", "Urgent", "High", "Normal", "Low"}
	cm.form.AddDropDown("Priority", priorities, 3, func(option string, index int) {
		cm.priority = index
		cm.priorityLabel = option
	})
	// Get the dropdown and style it
	if item := cm.form.GetFormItemByLabel("Priority"); item != nil {
		if dropdown, ok := item.(*tview.DropDown); ok {
			cm.priorityField = dropdown
		}
	}
	cm.priorityField.SetFieldWidth(50)
	cm.priorityField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)

	// Add action buttons
	cm.form.AddButton("Create", func() {
		var title, desc string
		if titleItem := cm.form.GetFormItemByLabel("Title"); titleItem != nil {
			if inputField, ok := titleItem.(*tview.InputField); ok {
				title = inputField.GetText()
			}
		}
		if descItem := cm.form.GetFormItemByLabel("Description"); descItem != nil {
			if textArea, ok := descItem.(*tview.TextArea); ok {
				desc = textArea.GetText()
			}
		}
		cm.Hide()
		if cm.onCreate != nil && title != "" {
			cm.onCreate(title, desc, cm.teamID, cm.projectID, cm.assigneeID, cm.cycleID, cm.priority)
		}
	})
	cm.form.AddButton("Cancel", func() {
		cm.Hide()
	})

	// Create header with instructions
	cm.headerView = tview.NewTextView()
	cm.headerView.SetText("Create New Issue")
	cm.headerView.SetTextColor(app.theme.Accent)
	cm.headerView.SetBackgroundColor(app.theme.HeaderBg)

	cm.parentView = tview.NewTextView()
	cm.parentView.SetText("")
	cm.parentView.SetTextColor(app.theme.SecondaryText)
	cm.parentView.SetBackgroundColor(app.theme.HeaderBg)

	// Create help text
	helpView := tview.NewTextView()
	helpView.SetText("Tab: next field • Enter: open dropdown • Esc: cancel")
	helpView.SetTextColor(app.theme.SecondaryText)
	helpView.SetBackgroundColor(app.theme.HeaderBg)
	helpView.SetTextAlign(tview.AlignCenter)

	// Build modal content
	modalContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(cm.headerView, 1, 0, false).
		AddItem(cm.parentView, 1, 0, false).
		AddItem(cm.form, 0, 1, true).
		AddItem(helpView, 1, 0, false)
	modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	modalContent.SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitle(" New Issue ").
		SetTitleColor(app.theme.Foreground)
	padding := app.density.ModalPadding
	modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	// Center the modal on screen and resize its fields when the terminal narrows.
	centered := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(modalContent, 20, 0, true).
		AddItem(nil, 0, 1, false)
	cm.modal = newResponsiveWorkspaceModal(app.theme.Background, centered, 75, cm.resizeFields)

	return cm
}

func (cm *CreateIssueModal) resizeFields(modalWidth int) {
	fieldWidth := modalWidth - 18
	if fieldWidth < 1 {
		fieldWidth = 1
	}
	if titleItem := cm.form.GetFormItemByLabel("Title"); titleItem != nil {
		if inputField, ok := titleItem.(*tview.InputField); ok {
			inputField.SetFieldWidth(fieldWidth)
		}
	}
	if descItem := cm.form.GetFormItemByLabel("Description"); descItem != nil {
		if textArea, ok := descItem.(*tview.TextArea); ok {
			textArea.SetSize(4, fieldWidth)
		}
	}
	if fieldWidth > 50 {
		fieldWidth = 50
	}
	for _, field := range []*tview.DropDown{cm.assigneeField, cm.cycleField, cm.priorityField} {
		field.SetFieldWidth(fieldWidth)
	}
}

// Show displays the create issue modal.
func (cm *CreateIssueModal) Show(teamID, projectID string, onCreate func(title, description, teamID, projectID, assigneeID, cycleID string, priority int)) {
	cm.ShowWithOptions(CreateIssueModalOptions{TeamID: teamID, ProjectID: projectID}, onCreate)
}

func (cm *CreateIssueModal) ShowWithOptions(options CreateIssueModalOptions, onCreate func(title, description, teamID, projectID, assigneeID, cycleID string, priority int)) {
	logger.Debug("tui.create_issue: showing create issue modal team_id=%s project_id=%s", options.TeamID, options.ProjectID)
	cm.teamID = options.TeamID
	cm.projectID = options.ProjectID
	cm.onCreate = onCreate
	cm.selectedCycle = options.CycleID
	if options.Parent != nil {
		cm.headerView.SetText("Create Sub-Issue")
		cm.parentView.SetText(fmt.Sprintf("Parent: %s - %s", options.Parent.Identifier, options.Parent.Title))
	} else {
		cm.headerView.SetText("Create New Issue")
		cm.parentView.SetText("")
	}

	// Reset form fields
	if titleItem := cm.form.GetFormItemByLabel("Title"); titleItem != nil {
		if inputField, ok := titleItem.(*tview.InputField); ok {
			_ = inputField.SetText("")
		}
	}
	if descItem := cm.form.GetFormItemByLabel("Description"); descItem != nil {
		if textArea, ok := descItem.(*tview.TextArea); ok {
			_ = textArea.SetText("", true)
		}
	}

	// Reset selections
	cm.assigneeID = ""
	cm.assigneeName = ""
	cm.assigneeField.SetCurrentOption(0)
	cm.cycleID = ""
	cm.cycleName = ""
	cm.cycleField.SetCurrentOption(0)
	cm.priority = 3 // Default to Normal
	cm.priorityLabel = "Normal"
	cm.priorityField.SetCurrentOption(3)

	// Show modal first with loading state for async fields.
	cm.assigneeField.SetOptions([]string{"Loading..."}, nil)
	cm.cycleField.SetOptions([]string{"Loading..."}, nil)
	cm.form.SetFocus(0)
	cm.app.pages.AddPage("create_issue", cm.modal, true, true)
	cm.app.pages.SendToFront("create_issue")
	cm.app.app.SetFocus(cm.form)

	// Load users asynchronously
	cm.loadUsers()
	cm.loadCycles()
}

// loadUsers fetches team users and populates the assignee dropdown.
func (cm *CreateIssueModal) loadUsers() {
	users := cm.app.GetTeamUsers()
	if len(users) > 0 {
		cm.populateAssigneeDropdown(users)
		return
	}

	// Users not loaded yet, fetch them
	go func() {
		loadedUsers, err := cm.app.FetchTeamUsers(cm.teamID)
		if err != nil {
			cm.app.app.QueueUpdateDraw(func() {
				cm.assigneeField.SetOptions([]string{"Unassigned", "(Failed to load users)"}, nil)
			})
			return
		}
		cm.app.app.QueueUpdateDraw(func() {
			cm.populateAssigneeDropdown(loadedUsers)
		})
	}()
}

// populateAssigneeDropdown fills the assignee dropdown with users.
func (cm *CreateIssueModal) populateAssigneeDropdown(users []linearapi.User) {
	assigneeOptions := []string{"Unassigned"}
	cm.cachedUsers = make([]struct{ ID, Name string }, 0, len(users))
	for _, user := range users {
		displayName := user.Name
		if user.IsMe {
			displayName = fmt.Sprintf("%s (me)", user.Name)
		}
		assigneeOptions = append(assigneeOptions, displayName)
		cm.cachedUsers = append(cm.cachedUsers, struct{ ID, Name string }{user.ID, displayName})
	}
	cm.assigneeField.SetOptions(assigneeOptions, func(_ string, index int) {
		if index == 0 {
			cm.assigneeID = ""
			cm.assigneeName = ""
		} else if index > 0 && index <= len(cm.cachedUsers) {
			user := cm.cachedUsers[index-1]
			cm.assigneeID = user.ID
			cm.assigneeName = user.Name
		}
	})
	cm.assigneeField.SetCurrentOption(0)
}

func (cm *CreateIssueModal) loadCycles() {
	cycles := cm.app.GetTeamCycles()
	if len(cycles) > 0 {
		cm.populateCycleDropdown(cycles)
		return
	}

	go func() {
		loadedCycles, err := cm.app.FetchTeamCycles(cm.teamID)
		if err != nil {
			cm.app.app.QueueUpdateDraw(func() {
				cm.cycleField.SetOptions([]string{"No cycle", "(Failed to load cycles)"}, nil)
			})
			return
		}
		cm.app.app.QueueUpdateDraw(func() {
			cm.populateCycleDropdown(loadedCycles)
		})
	}()
}

func (cm *CreateIssueModal) populateCycleDropdown(cycles []linearapi.Cycle) {
	cycleOptions := []string{"No cycle"}
	cm.cachedCycles = make([]struct{ ID, Name string }, 0, len(cycles))
	selectedIndex := 0
	for _, cycle := range cycles {
		displayName := cycle.DisplayName()
		switch {
		case cycle.IsActive:
			displayName += " (active)"
		case cycle.IsNext:
			displayName += " (next)"
		case cycle.IsPrevious:
			displayName += " (previous)"
		}
		cycleOptions = append(cycleOptions, displayName)
		cm.cachedCycles = append(cm.cachedCycles, struct{ ID, Name string }{cycle.ID, displayName})
		if cycle.ID == cm.selectedCycle {
			selectedIndex = len(cycleOptions) - 1
		}
	}
	cm.cycleField.SetOptions(cycleOptions, func(_ string, index int) {
		if index == 0 {
			cm.cycleID = ""
			cm.cycleName = ""
		} else if index > 0 && index <= len(cm.cachedCycles) {
			cycle := cm.cachedCycles[index-1]
			cm.cycleID = cycle.ID
			cm.cycleName = cycle.Name
		}
	})
	cm.cycleField.SetCurrentOption(selectedIndex)
	if selectedIndex == 0 {
		cm.cycleID = ""
		cm.cycleName = ""
	} else {
		cycle := cm.cachedCycles[selectedIndex-1]
		cm.cycleID = cycle.ID
		cm.cycleName = cycle.Name
	}
}

// Hide hides the create issue modal.
func (cm *CreateIssueModal) Hide() {
	cm.app.pages.RemovePage("create_issue")
	cm.app.updateFocus()
}

// HandleKey handles keyboard input for the create issue modal.
func (cm *CreateIssueModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		if cm.closeOpenDropdown(event) {
			return nil
		}
		cm.Hide()
		return nil
	}
	return event
}

func (cm *CreateIssueModal) closeOpenDropdown(event *tcell.EventKey) bool {
	for _, dropdown := range []*tview.DropDown{cm.assigneeField, cm.cycleField, cm.priorityField} {
		if dropdown == nil || !dropdown.IsOpen() {
			continue
		}
		if handler := dropdown.InputHandler(); handler != nil {
			handler(event, func(p tview.Primitive) {
				cm.app.app.SetFocus(p)
			})
		}
		return true
	}
	return false
}

// GetModal returns the modal flex for adding to pages.
func (cm *CreateIssueModal) GetModal() *tview.Flex {
	return cm.modal
}
