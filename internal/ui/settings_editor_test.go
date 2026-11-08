package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/settings"
)

func editableSettingsModel(t *testing.T) noseyModel {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := settings.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	m := productModel()
	m.tab = 3
	m.session.Store = store
	m.session.Resolved.Settings = settings.Defaults()
	return m
}
func TestSettingsChoicesStayOnPageAndSaveTypedValues(t *testing.T) {
	for _, tc := range []struct{ id, key string }{{"display", "ui.mode"}, {"method", "installation.method"}, {"mode", "installation.defaultMode"}, {"telemetry", "installation.upstreamTelemetry"}, {"mcp", "detection.enableMcpPackage"}, {"verbose", "installation.verbose"}} {
		t.Run(tc.id, func(t *testing.T) {
			m := editableSettingsModel(t)
			m, _ = m.setting(noseyRow{kind: "setting", id: tc.id})
			if len(m.form.choices[0]) < 2 {
				t.Fatal("fixed-choice setting is a text field")
			}
			screen := ansi.Strip(m.View().Content)
			if !strings.Contains(screen, "[Settings]") || (!strings.Contains(screen, "Refresh interval:") && !strings.Contains(screen, "Maximum inspected file:") && !strings.Contains(screen, "Default install action:")) {
				t.Fatal("settings form replaced its parent screen", screen)
			}
			m = productUpdate(t, m, keypress(tea.KeyDown))
			want := m.form.inputs[0].Value()
			m = productUpdate(t, m, keypress(tea.KeyEnter))
			r, err := m.session.Store.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			switch tc.id {
			case "display":
				got = r.Settings.UI.Mode
			case "method":
				got = r.Settings.Installation.Method
			case "mode":
				got = r.Settings.Installation.Mode
			case "telemetry":
				got = r.Settings.Installation.Telemetry
			case "mcp":
				if r.Settings.Detection.MCP {
					got = "true"
				} else {
					got = "false"
				}
			case "verbose":
				if r.Settings.Installation.Verbose {
					got = "true"
				} else {
					got = "false"
				}
			}
			if got != want || m.modal != "" || m.tab != 3 {
				t.Fatal("choice did not save in its settings context", tc.key, got, want)
			}
		})
	}
}
func TestSettingsAgentMultiselectAndCancellation(t *testing.T) {
	m := editableSettingsModel(t)
	m, _ = m.setting(noseyRow{kind: "setting", id: "agents"})
	if !m.form.multiple[0] || len(m.form.choices[0]) < 10 {
		t.Fatal("agents did not use pinned multiselect options")
	}
	first := m.form.choices[0][m.form.choiceIndex[0]].id
	m = productUpdate(t, m, keypress(tea.KeySpace))
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	r, err := m.session.Store.Resolve()
	if err != nil || len(r.Settings.Installation.Agents) != 1 || r.Settings.Installation.Agents[0] != first {
		t.Fatal("agent multiselect did not save", r.Settings.Installation.Agents, err)
	}
	m.loading = false
	m, _ = m.setting(noseyRow{kind: "setting", id: "method"})
	m = productUpdate(t, m, keypress(tea.KeyDown))
	m = productUpdate(t, m, keypress(tea.KeyEsc))
	r, err = m.session.Store.Resolve()
	if err != nil || r.Settings.Installation.Method != "symlink" {
		t.Fatal("cancel saved a draft", err)
	}
}
func TestSettingsRetainFreeformInputsAndTemplateChoices(t *testing.T) {
	m := editableSettingsModel(t)
	for _, id := range []string{"ttl", "bytes", "exclude"} {
		m, _ = m.setting(noseyRow{kind: "setting", id: id})
		if len(m.form.choices[0]) != 0 {
			t.Fatal("custom number or pattern lost its input", id)
		}
	}
	m, _ = m.setting(noseyRow{kind: "setting", id: "new"})
	for _, field := range []int{2, 3, 4} {
		if len(m.form.choices[field]) != 2 {
			t.Fatal("template/boolean field remained freeform", field)
		}
	}
	if m.settingsAnchor != "new" || !strings.Contains(ansi.Strip(m.View().Content), "New registry") {
		t.Fatal("registry form lost its settings anchor")
	}
}

func TestAgentMultiselectDoesNotTruncateLargeSelections(t *testing.T) {
	m := editableSettingsModel(t)
	m, _ = m.setting(noseyRow{kind: "setting", id: "agents"})
	count := len(m.form.choices[0])
	for i := 0; i < count; i++ {
		m = productUpdate(t, m, keypress(tea.KeySpace))
		m = productUpdate(t, m, keypress(tea.KeyDown))
	}
	if len(csv(m.form.inputs[0].Value())) != count {
		t.Fatal("agent multiselect truncated IDs")
	}
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	r, err := m.session.Store.Resolve()
	if err != nil || len(r.Settings.Installation.Agents) != count {
		t.Fatal("agent multiselect lost selected agents", err)
	}
}
