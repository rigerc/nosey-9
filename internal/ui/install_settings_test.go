package ui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

type installSettingsRunner struct{ calls int }

func (r *installSettingsRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	r.calls++
	return process.Output{}, nil
}

func TestInstallUsesSettingsAndGoesStraightToReview(t *testing.T) {
	for _, action := range []string{"execute", "print"} {
		t.Run(action, func(t *testing.T) {
			m := productModel()
			cfg := settings.Defaults()
			cfg.Installation.Agents = []string{"codex", "claude-code"}
			cfg.Installation.Method = "copy"
			cfg.Installation.Mode = action
			source := t.TempDir()
			if err := storage.Write(filepath.Join(source, "skill/SKILL.md"), []byte("---\nname: fixture\n---\nBody\n")); err != nil {
				t.Fatal(err)
			}
			runner := &installSettingsRunner{}
			store := settings.Store{Workspace: t.TempDir(), Data: t.TempDir()}
			m.session.Resolved.Settings = cfg
			m.session.Installer = installation.Service{Store: store, Runner: runner}
			m.session.Catalog = domain.Catalog{Skills: map[string]domain.Skill{"test:fixture": {ID: "test:fixture", RegistryID: "test:registry", Source: domain.Source{Kind: "local", Path: "skill", SkillName: "fixture"}}}, Roots: map[string]string{"test:registry": source}}
			m.selected = map[string]bool{"test:fixture": true}
			m.filters[0].Query = "hidden"
			next, cmd := m.installFromSettings()
			if cmd == nil || next.modal != "" || !next.loading {
				t.Fatal("installation still opened a preferences form")
			}
			message, ok := cmd().(noseyPlan)
			if !ok || message.err != nil {
				t.Fatal("configured plan failed", message.err)
			}
			if message.plan.Method != "copy" || !slices.Equal(message.plan.Agents, []string{"claude-code", "codex"}) || !slices.Contains(message.plan.Commands[0].Argv, "--copy") {
				t.Fatal("installation ignored configured agents/method", message.plan)
			}
			updated, _ := next.Update(message)
			review, ok := updated.(noseyModel)
			if !ok || review.modal != "plan" || review.action != action {
				t.Fatal("configured action did not lead to plan review")
			}
			if runner.calls != 0 {
				t.Fatal("planning invoked the installer")
			}
			if _, err := os.Stat(filepath.Join(store.Data, "install-sources")); !os.IsNotExist(err) {
				t.Fatal("review prepared sources before confirmation")
			}
			// Source approval remains required in execute mode, and print mode never executes.
			review = productUpdate(t, review, keypress(tea.KeyEnter))
			if runner.calls != 0 {
				t.Fatal("settings bypassed reviewed approval")
			}
		})
	}
}
func TestMissingInstallationAgentsPointsToSettingsWithoutPrompt(t *testing.T) {
	m := productModel()
	m.selected["s"] = true
	m = productUpdate(t, m, keypress('i'))
	if m.modal != "" || m.loading || m.err == nil || !strings.Contains(m.err.Error(), "Settings") {
		t.Fatal("missing agents did not give a settings error")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "choose target agents in Settings") {
		t.Fatal("missing-agent error was not visible")
	}
}
