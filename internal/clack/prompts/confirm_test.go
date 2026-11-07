package prompts_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/stretchr/testify/assert"
)

func TestConfirmInitialState(t *testing.T) {
	p := prompts.ConfirmModel(prompts.ConfirmParams{Message: message})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := strings.Join([]string{
		symbols.Bar, symbols.RadioInactive, p.Active, "/", symbols.RadioActive, p.Inactive,
	}, " ")
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestConfirmInitialStateWithInitialValue(t *testing.T) {
	p := prompts.ConfirmModel(prompts.ConfirmParams{Message: message, InitialValue: true})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	valueWithCursor := strings.Join([]string{
		symbols.Bar, symbols.RadioActive, p.Active, "/", symbols.RadioInactive, p.Inactive,
	}, " ")
	expected := strings.Join([]string{symbols.Bar, title, valueWithCursor, symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestConfirmCancelState(t *testing.T) {
	p := prompts.ConfirmModel(prompts.ConfirmParams{Message: message})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message,
		symbols.Bar + " " + p.Inactive, symbols.Bar,
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestConfirmCancelStateWithValue(t *testing.T) {
	p := prompts.ConfirmModel(prompts.ConfirmParams{Message: message, InitialValue: true})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message,
		symbols.Bar + " " + p.Active, symbols.Bar,
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestConfirmSubmitState(t *testing.T) {
	p := prompts.ConfirmModel(prompts.ConfirmParams{Message: message})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.SubmitState) + " " + message,
		symbols.Bar + " " + p.Inactive,
	}, "\r\n")
	assert.Equal(t, core.SubmitState, p.State)
	assert.False(t, p.Value)
	assert.Equal(t, expected, p.Frame)
}

func TestConfirmToggleAndSubmit(t *testing.T) {
	p := prompts.ConfirmModel(prompts.ConfirmParams{Message: message})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyRight})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.True(t, p.Value)
}
