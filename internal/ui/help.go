package ui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
)

type staticHelp struct {
	short []key.Binding
	full  [][]key.Binding
}

func (h staticHelp) ShortHelp() []key.Binding {
	return h.short
}

func (h staticHelp) FullHelp() [][]key.Binding {
	return h.full
}

// ListHelpView renders the help bar for a list using the supplied bindings.
func ListHelpView(model list.Model, short []key.Binding, full [][]key.Binding) string {
	return model.Styles.HelpStyle.Render(model.Help.View(staticHelp{
		short: short,
		full:  full,
	}))
}
