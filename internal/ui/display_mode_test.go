package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/settings"
)

func TestDisplayModeSizingAndSmallTerminalFallback(t *testing.T) {
	for _, mode := range []string{"inline", "fullscreen"} {
		m := productModel()
		m.displayMode = mode
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
		next, ok := updated.(noseyModel)
		if !ok {
			t.Fatal("unexpected model")
		}
		expected := 50
		if mode == "inline" {
			expected = 24
		}
		if next.height != expected || next.View().AltScreen != (mode == "fullscreen") {
			t.Fatal("incorrect display mode or sizing", mode, next.height)
		}
		updated, _ = next.Update(tea.WindowSizeMsg{Width: 20, Height: 4})
		next, ok = updated.(noseyModel)
		if !ok {
			t.Fatal("unexpected model")
		}
		if next.View().AltScreen != (mode == "fullscreen") {
			t.Fatal("small-terminal fallback changed display mode")
		}
	}
}
func TestDisplaySelectSavesForNextLaunch(t *testing.T) {
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
	m.openDisplaySelect()
	m = productUpdate(t, m, keypress(tea.KeyDown))
	m = productUpdate(t, m, keypress(tea.KeyEnter))
	r, err := store.Resolve()
	if err != nil || r.Settings.UI.Mode != "inline" || m.modal != "" {
		t.Fatal("display select did not persist preference", err)
	}
	if m.displayMode != "fullscreen" || !m.View().AltScreen {
		t.Fatal("saving preference unexpectedly changed active terminal buffer")
	}
}

// Start directly with prepared data; terminal tests must never fetch a registry or invoke an installer.
type displayFixture struct{ noseyModel }

func (displayFixture) Init() tea.Cmd { return nil }

func TestInlineTerminalOutputPreservesNormalBuffer(t *testing.T) {
	for _, mode := range []string{"inline", "fullscreen"} {
		t.Run(mode, func(t *testing.T) {
			m := productModel()
			m.displayMode = mode
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var output bytes.Buffer
			output.WriteString("shell history sentinel\n")
			program := tea.NewProgram(displayFixture{m}, tea.WithContext(ctx), tea.WithInput(strings.NewReader("q")), tea.WithOutput(&output), tea.WithWindowSize(80, 40), tea.WithEnvironment([]string{"TERM=xterm-256color", "NO_COLOR=1"}), tea.WithoutSignalHandler())
			if _, err := program.Run(); err != nil {
				t.Fatal(err)
			}
			captured := output.String()
			if mode == "inline" {
				for _, sequence := range []string{"\x1b[?1049h", "\x1b[?1047h", "\x1b[?47h", "\x1b[2J", "\x1b[3J"} {
					if strings.Contains(captured, sequence) {
						t.Fatalf("inline output changed terminal buffer with %q", sequence)
					}
				}
				if !strings.HasPrefix(captured, "shell history sentinel\n") || !strings.Contains(captured, ":^) nosey") {
					t.Fatal("inline view was not left in normal output")
				}
			} else if !strings.Contains(captured, "\x1b[?1049h") {
				t.Fatal("fullscreen did not enter alternate screen")
			}
			if !strings.Contains(captured, "\x1b[?25h") {
				t.Fatal("terminal cursor was not restored")
			}
		})
	}
}
