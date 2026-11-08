package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/ui"
)

func TestRootCommandHasVersion(t *testing.T) {
	cmd := newRootCmd()
	versionFlag := cmd.Flags().Lookup("version")
	if versionFlag == nil {
		t.Fatal("expected --version flag to be registered")
	}
	if versionFlag.Shorthand != "v" {
		t.Fatalf("expected shorthand -v, got %q", versionFlag.Shorthand)
	}
}

func TestRootCommandHasConfig(t *testing.T) {
	cmd := newRootCmd()
	configFlag := cmd.PersistentFlags().Lookup("config")
	if configFlag == nil {
		t.Fatal("expected --config flag to be registered")
	}
}

func TestResolvedVersion(t *testing.T) {
	ver := resolvedVersion()
	if ver == "" {
		t.Fatal("expected version to be non-empty")
	}
}

func TestRootCommandRejectsPositionalArguments(t *testing.T) {
	cmd := newRootCmd()
	if err := cmd.Args(cmd, nil); err != nil {
		t.Fatalf("root should accept no arguments: %v", err)
	}
	if err := cmd.Args(cmd, []string{"typo"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}

func TestDirectoryListFilterKeysAreForwarded(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "q", key: press('q')},
		{name: "a", key: press('a')},
		{name: "d", key: press('d')},
		{name: "r", key: press('r')},
		{name: "o", key: press('o')},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newTestDirectoryModel(t)
			model = updateDirectoryModel(t, model, press('/'))
			if !model.list.SettingFilter() {
				t.Fatal("expected / to start filtering")
			}

			model, _ = updateDirectoryModelWithCmd(t, model, tt.key)
			if model.list.FilterValue() != tt.name {
				t.Fatalf("FilterValue() = %q, want %q", model.list.FilterValue(), tt.name)
			}
			if model.message != "" {
				t.Fatalf("message = %q, want empty while filtering", model.message)
			}
		})
	}
}

func TestDirectoryListEscCancelsFilter(t *testing.T) {
	model := newTestDirectoryModel(t)
	model = updateDirectoryModel(t, model, press('/'))
	model = updateDirectoryModel(t, model, press('a'))

	model, cmd := updateDirectoryModelWithCmd(t, model, special(tea.KeyEsc))
	if isQuitCommand(cmd) {
		t.Fatal("esc while filtering should not quit")
	}
	if model.list.IsFiltered() {
		t.Fatal("expected esc to clear the active filter")
	}
	if model.list.FilterValue() != "" {
		t.Fatalf("FilterValue() = %q, want empty", model.list.FilterValue())
	}
}

func TestDirectoryListEnterAppliesFilter(t *testing.T) {
	model := newTestDirectoryModel(t)
	model = updateDirectoryModel(t, model, press('/'))
	for _, r := range "alpha" {
		model = updateDirectoryModel(t, model, press(r))
	}

	model = updateDirectoryModel(t, model, special(tea.KeyEnter))
	if model.list.SettingFilter() {
		t.Fatal("expected enter to apply the filter")
	}
	if model.message != "" {
		t.Fatalf("message = %q, want empty while applying a filter", model.message)
	}
}

func TestDirectoryListQuitAndSelection(t *testing.T) {
	t.Run("q quits outside filtering", func(t *testing.T) {
		model := newTestDirectoryModel(t)
		_, cmd := updateDirectoryModelWithCmd(t, model, press('q'))
		if !isQuitCommand(cmd) {
			t.Fatal("expected q to quit the program")
		}
	})

	t.Run("enter reports the selected item", func(t *testing.T) {
		model := newTestDirectoryModel(t)
		model = updateDirectoryModel(t, model, special(tea.KeyEnter))
		if model.message != "Selected: alpha" {
			t.Fatalf("message = %q, want Selected: alpha", model.message)
		}
	})
}

func TestLoadRootConfigReturnsErrors(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(badPath, []byte("editor = [\n"), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	if _, err := loadRootConfig(dir, &rootOptions{configPath: badPath}); err == nil {
		t.Fatal("expected malformed override config to return an error")
	}
	if _, err := loadRootConfig(dir, &rootOptions{configPath: filepath.Join(dir, "missing.toml")}); err == nil {
		t.Fatal("expected missing override config to return an error")
	}

	goodPath := filepath.Join(dir, "good.toml")
	if err := os.WriteFile(goodPath, []byte("editor = \"emacs\"\n"), 0o600); err != nil {
		t.Fatalf("write good config: %v", err)
	}
	cfg, err := loadRootConfig(dir, &rootOptions{configPath: goodPath})
	if err != nil {
		t.Fatalf("load valid override: %v", err)
	}
	if cfg.Editor != "emacs" {
		t.Fatalf("Editor = %q, want emacs", cfg.Editor)
	}
}

func TestRequireInteractiveRejectsPipes(t *testing.T) {
	in, inWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create input pipe: %v", err)
	}
	defer func() { _ = in.Close() }()
	defer func() { _ = inWriter.Close() }()

	out, outReader, err := os.Pipe()
	if err != nil {
		t.Fatalf("create output pipe: %v", err)
	}
	defer func() { _ = out.Close() }()
	defer func() { _ = outReader.Close() }()

	err = requireInteractive(in, out)
	if err == nil {
		t.Fatal("expected a non-terminal pipe to be rejected")
	}
	if !strings.Contains(err.Error(), "interactive terminal required") {
		t.Fatalf("error = %q, want an interactive terminal message", err)
	}
}

func newTestDirectoryModel(t *testing.T) directoryListModel {
	t.Helper()
	lm := list.New([]list.Item{
		fileItem{name: "alpha"},
		fileItem{name: "beta"},
	}, list.NewDefaultDelegate(), 80, 20)
	lm.SetFilteringEnabled(true)
	lm.DisableQuitKeybindings()
	return directoryListModel{list: lm}
}

func updateDirectoryModel(t *testing.T, model directoryListModel, key tea.KeyPressMsg) directoryListModel {
	t.Helper()
	model, _ = updateDirectoryModelWithCmd(t, model, key)
	return model
}

func updateDirectoryModelWithCmd(t *testing.T, model directoryListModel, key tea.KeyPressMsg) (directoryListModel, tea.Cmd) {
	t.Helper()
	updated, cmd := model.Update(key)
	next, ok := updated.(directoryListModel)
	if !ok {
		t.Fatalf("Update() returned %T, want directoryListModel", updated)
	}
	return next, cmd
}

func press(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

func special(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func isQuitCommand(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestHelpKeysAtAllWidths(t *testing.T) {
	t.Helper()
	m := directoryListModel{responsive: ui.NewResponsiveManager(80)}
	for _, width := range []int{20, 60, 100, 140, 200} {
		m.responsive.SetWidth(width)
		_ = m.getShortHelpKeys()
		_ = m.getFullHelpKeys()
	}
}
