package ui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

// ConfigureTextarea applies shared styles and keybindings for multi-line input.
func ConfigureTextarea(input *textarea.Model, theme Theme, altEnterSubmit bool) (key.Binding, key.Binding) {
	if input == nil {
		return key.Binding{}, key.Binding{}
	}
	input.ShowLineNumbers = false
	styles := input.Styles()
	styles.Focused.Base = lipgloss.NewStyle().Foreground(theme.Text)
	styles.Blurred.Base = lipgloss.NewStyle().Foreground(theme.Text)
	input.SetStyles(styles)
	submitKey, newlineKey := TextareaKeyBindings(altEnterSubmit)
	input.KeyMap.InsertNewline = newlineKey
	return submitKey, newlineKey
}

// TextareaKeyBindings returns submit and newline bindings based on config.
func TextareaKeyBindings(altEnterSubmit bool) (key.Binding, key.Binding) {
	submitKey := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit"))
	newlineKey := key.NewBinding(
		key.WithKeys("alt+enter"),
		key.WithHelp("alt+enter", "newline"),
	)
	if altEnterSubmit {
		submitKey = key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("alt+enter", "submit"))
		newlineKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "newline"))
	}
	return submitKey, newlineKey
}

// TextareaSubmitHelp formats the key help for a multi-line submit action.
func TextareaSubmitHelp(submitKey key.Binding, action string) string {
	if submitKey.Help().Key == "alt+enter" {
		return fmt.Sprintf("Press Alt+Enter to %s. Enter for a new line.", action)
	}
	return fmt.Sprintf("Press Enter to %s. Alt+Enter for a new line.", action)
}
