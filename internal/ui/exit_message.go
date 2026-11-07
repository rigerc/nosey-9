package ui

import "charm.land/lipgloss/v2"

// ClipboardConfirm renders the standard clipboard confirmation message.
func ClipboardConfirm(theme Theme) string {
	return ExitMessage(theme, "  Copied to clipboard  󱁖", false)
}

// ExitMessage renders a standard framed exit message.
func ExitMessage(theme Theme, message string, mutedText bool) string {
	style := lipgloss.NewStyle().
		Foreground(theme.Text).
		Margin(0, 2, 0, 2)

	if mutedText {
		return style.Render(message)
	}

	return style.Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Muted).
		Foreground(theme.Secondary).
		Bold(true).
		Margin(1, 1).
		Padding(1, 2).Render(message)
}
