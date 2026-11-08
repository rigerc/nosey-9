package prompts_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/rigerc/nosey-9/internal/clack/prompts/symbols"
	"github.com/stretchr/testify/assert"
)

func TestPathInitialState(t *testing.T) {
	p := prompts.PathModel(prompts.PathParams{Message: message})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := symbols.Bar + " " + p.ValueWithCursor()
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPathInitialStateWithInitialValue(t *testing.T) {
	p := prompts.PathModel(prompts.PathParams{Message: message, InitialValue: "/foo"})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := symbols.Bar + " /foo█"
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPathErrorState(t *testing.T) {
	p := prompts.PathModel(prompts.PathParams{
		Message: message, InitialValue: "/foo",
		Validate: func(value string) error { return fmt.Errorf("invalid value: %s", value) },
	})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	waitForPromptOutput(t, tm, "invalid value: /foo")
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, "/foo", p.Value)
}

func TestPathCancelState(t *testing.T) {
	p := prompts.PathModel(prompts.PathParams{Message: message})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
}

func TestPathCancelStateWithValue(t *testing.T) {
	p := prompts.PathModel(prompts.PathParams{Message: message, InitialValue: "/foo"})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message,
		symbols.Bar + " /foo", symbols.Bar,
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPathSubmitState(t *testing.T) {
	p := prompts.PathModel(prompts.PathParams{Message: message, InitialValue: "/foo"})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.SubmitState) + " " + message, symbols.Bar + " /foo",
	}, "\r\n")
	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "/foo", p.Value)
	assert.Equal(t, expected, p.Frame)
}

func TestPathValueWithOptions(t *testing.T) {
	// Keep this as a deterministic renderer assertion; program-level path
	// navigation and autocomplete will be covered by the selection migration.
	p := prompts.PathModel(prompts.PathParams{Message: message})
	p.Init()
	p.PressKey(&core.Key{Name: core.TabKey})
	p.PressKey(&core.Key{Name: core.TabKey})

	title := symbols.State(core.ActiveState) + " " + message
	// The initial value is the working directory, whose length depends on the checkout path, so wrap
	// it with the same renderer instead of assuming it fits on one line.
	lineOptions := core.FormatLinesOptions{Default: core.FormatLineOptions{Start: symbols.Bar}}
	value := p.FormatLines([]string{p.ValueWithCursor()}, lineOptions)
	options := p.FormatLines([]string{strings.Join(p.HintOptions, " ")}, lineOptions)
	expected := strings.Join([]string{symbols.Bar, title, value, options, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.ActiveState, p.State)
	assert.Equal(t, expected, p.Frame)
}
