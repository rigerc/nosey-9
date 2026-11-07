package prompts_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/stretchr/testify/assert"
)

func TestPasswordInitialState(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{Message: message})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	expected := strings.Join([]string{symbols.Bar, title, symbols.Bar + " █", symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPasswordInitialStateWithInitialValue(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{Message: message, InitialValue: "foo"})
	p.Init()

	title := symbols.State(core.InitialState) + " " + message
	expected := strings.Join([]string{symbols.Bar, title, symbols.Bar + " ***█", symbols.BarEnd}, "\r\n")
	assert.Equal(t, core.InitialState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPasswordErrorState(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{
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

func TestPasswordCancelState(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{Message: message})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message, symbols.Bar + " ",
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPasswordCancelStateWithValue(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{Message: message, InitialValue: "foo"})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.CancelState) + " " + message,
		symbols.Bar + " ***", symbols.Bar,
	}, "\r\n")
	assert.Equal(t, core.CancelState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPasswordSubmitState(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{Message: message, InitialValue: "foo"})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	expected := strings.Join([]string{
		symbols.Bar, symbols.State(core.SubmitState) + " " + message, symbols.Bar + " ***",
	}, "\r\n")
	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, expected, p.Frame)
}

func TestPasswordTypesWithoutRenderingSecret(t *testing.T) {
	p := prompts.PasswordModel(prompts.PasswordParams{Message: message})
	tm := startTestPrompt(t, p, message)
	tm.Type("s3cret")
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "s3cret", p.Value)
	assert.NotContains(t, p.Frame, "s3cret")
	assert.Contains(t, p.Frame, "******")
}
