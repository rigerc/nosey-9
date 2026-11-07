package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts"
)

// ConfirmationModel is a reusable confirmation dialog backed by go-clack's
// native Bubble Tea confirm prompt. It keeps the template's framed dialog
// presentation while using the migrated prompt runtime for input handling,
// validation, and terminal rendering.
//
// Example usage as a standalone dialog:
//
//	confirmed, err := ui.PromptConfirmation("Delete file?", "Are you sure?", theme)
//	if err != nil {
//	    return err
//	}
//	if confirmed {
//	    // perform action
//	}
//
// Example usage embedded in another Bubble Tea model:
//
//	// In your model struct:
//	type myModel struct {
//	    confirmMode bool
//	    confirm     *ui.ConfirmationModel
//	}
//
//	// To show the dialog:
//	confirmModel := ui.NewConfirmationModel("Delete?", "Are you sure?", theme)
//	m.confirmMode = true
//	m.confirm = &confirmModel
//	return m, confirmModel.Init()
//
//	// In Update method:
//	if m.confirmMode {
//	    updated, cmd := m.confirm.Update(msg)
//	    confirm := updated.(ui.ConfirmationModel)
//	    m.confirm = &confirm
//	    if confirm.Done() {
//	        // read confirm.ChoiceValue() and leave confirmMode
//	    }
//	    return m, cmd
//	}
//
//	// In View method:
//	if m.confirmMode && m.confirm != nil {
//	    return tea.NewView(m.confirm.Render())
//	}
type ConfirmationModel struct {
	title   string
	prompt  string
	theme   Theme
	confirm *core.ConfirmPrompt
}

// NewConfirmationModel creates a new confirmation dialog.
func NewConfirmationModel(title, prompt string, theme Theme) ConfirmationModel {
	return ConfirmationModel{
		title:  title,
		prompt: prompt,
		theme:  theme,
		confirm: prompts.ConfirmModel(prompts.ConfirmParams{
			Message: prompt,
		}),
	}
}

// Init initializes the confirmation dialog.
func (m ConfirmationModel) Init() tea.Cmd {
	if m.confirm == nil {
		return nil
	}
	return m.confirm.Init()
}

// Update handles messages for the confirmation dialog.
func (m ConfirmationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.confirm == nil {
		return m, nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch keyMsg.String() {
		case "y", "Y":
			m.confirm.Value = true
			return m, m.submitWithEnter()
		case "n", "N":
			m.confirm.Value = false
			return m, m.submitWithEnter()
		}
	}
	cmd := m.confirm.UpdateEmbedded(msg)
	if m.confirm.Finished() {
		return m, tea.Quit
	}
	return m, cmd
}

// submitWithEnter confirms the current choice after a y/n shortcut.
func (m ConfirmationModel) submitWithEnter() tea.Cmd {
	cmd := m.confirm.UpdateEmbedded(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.confirm.Finished() {
		return tea.Quit
	}
	return cmd
}

// Done reports whether the user submitted or cancelled the dialog.
func (m ConfirmationModel) Done() bool {
	return m.confirm != nil && m.confirm.Finished()
}

// View renders the confirmation dialog as a top-level Bubble Tea view.
func (m ConfirmationModel) View() tea.View {
	return tea.NewView(m.Render())
}

// Render renders the confirmation dialog body for embedding in another view.
func (m ConfirmationModel) Render() string {
	titleText := m.title
	if strings.TrimSpace(titleText) == "" {
		titleText = "Confirm"
	}
	helpText := "Press y or n, or use the arrow keys and Enter."

	title := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Headings).Render(titleText)
	help := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(helpText)

	confirmView := ""
	if m.confirm != nil {
		confirmView = strings.TrimRight(m.confirm.BodyView().Content, "\n")
	}
	if strings.TrimSpace(confirmView) == "" {
		confirmView = lipgloss.NewStyle().Foreground(m.theme.Text).Render(m.prompt)
	}

	content := strings.Join([]string{
		title,
		"",
		confirmView,
		"",
		help,
	}, "\n")

	return lipgloss.NewStyle().
		Margin(1, 1).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Border).
		Render(content)
}

// ChoiceValue returns the user's choice.
func (m ConfirmationModel) ChoiceValue() bool {
	if m.confirm == nil {
		return false
	}
	return m.confirm.Value
}

// PromptConfirmation runs a confirmation dialog and returns the user's choice.
func PromptConfirmation(title, prompt string, theme Theme) (bool, error) {
	model := NewConfirmationModel(title, prompt, theme)
	program := tea.NewProgram(model, tea.WithoutSignalHandler())
	result, err := program.Run()
	if err != nil {
		return false, err
	}
	if m, ok := result.(ConfirmationModel); ok {
		return m.ChoiceValue(), nil
	}
	return false, fmt.Errorf("unexpected model result")
}
