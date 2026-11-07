package prompts_test

import (
	"bytes"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const promptTestTimeout = 5 * time.Second

// startTestPrompt exercises a real Bubble Tea program, not the legacy PressKey
// reducer. Readiness is observed from the terminal output before sending keys.
func startTestPrompt(t *testing.T, model tea.Model, title string) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })
	waitForPromptOutput(t, tm, title)
	return tm
}

func waitForPromptOutput(t *testing.T, tm *teatest.TestModel, want string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(want))
	}, teatest.WithDuration(promptTestTimeout), teatest.WithCheckInterval(10*time.Millisecond))
}

func sendPromptKey(tm *teatest.TestModel, key tea.Key) {
	tm.Send(tea.KeyPressMsg(key))
}

func finishTestPrompt(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.WaitFinished(t, teatest.WithFinalTimeout(promptTestTimeout))
}
