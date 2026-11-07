package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestConfirmationModelShortcuts(t *testing.T) {
	tests := []struct {
		name    string
		key     rune
		want    bool
		contain string
	}{
		{name: "yes", key: 'y', want: true, contain: "yes"},
		{name: "no", key: 'n', want: false, contain: "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewConfirmationModel("Delete File", "Are you sure?", Theme{})
			model.Init()

			updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tt.key}))
			confirm, ok := updated.(ConfirmationModel)
			if !ok {
				t.Fatalf("Update returned %T", updated)
			}
			if !confirm.Done() {
				t.Fatal("dialog did not finish after shortcut")
			}
			if confirm.ChoiceValue() != tt.want {
				t.Fatalf("ChoiceValue() = %v, want %v", confirm.ChoiceValue(), tt.want)
			}
			if rendered := confirm.Render(); !strings.Contains(rendered, "Delete File") || !strings.Contains(rendered, tt.contain) {
				t.Fatalf("Render() = %q", rendered)
			}
		})
	}
}
