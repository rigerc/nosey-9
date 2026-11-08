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

const message = "test message"

func TestTextInitialState(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := symbols.Bar + " █"
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestTextInitialStateWithPlaceholder(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message, Placeholder: "foo"})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := symbols.Bar + " foo"
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestTextInitialStateWithInitialValue(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message, InitialValue: "foo"})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := symbols.Bar + " foo█"
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestTextErrorState(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{
		Message: message, InitialValue: "foo",
		Validate: func(value string) error { return fmt.Errorf("invalid value: %s", value) },
	})
	tm := startTestPrompt(t, p, message)

	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	waitForPromptOutput(t, tm, "invalid value: foo")
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, "foo", p.Value)
}

func TestTextCancelState(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message, symbols.Bar + " ",
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestTextCancelStateWithValue(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message, InitialValue: "foo"})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message,
		symbols.Bar + " foo", symbols.Bar,
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, "foo", p.Value)
	assert.Equal(t, expected, p.Frame)
}

func TestTextSubmitState(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message, InitialValue: "foo"})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.SubmitState) + " " + message, symbols.Bar + " foo",
	}, "\r\n")
	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestTextTypesUTF8AndSubmits(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{Message: message, Required: true})
	tm := startTestPrompt(t, p, message)
	tm.Type("é世")
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "é世", p.Value)
}

func TestTextValidationRecoversThroughProgram(t *testing.T) {
	p := prompts.TextModel(prompts.TextParams{
		Message: message, InitialValue: "bad",
		Validate: func(value string) error {
			if value == "bad" {
				return fmt.Errorf("invalid value")
			}
			return nil
		},
	})
	tm := startTestPrompt(t, p, message)

	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	waitForPromptOutput(t, tm, "invalid value")
	for range 3 {
		sendPromptKey(tm, tea.Key{Code: tea.KeyBackspace})
	}
	tm.Type("Ada")
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "Ada", p.Value)
}
