package prompts_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bradleyjkemp/cupaloy"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func newSelectKeyModel() *core.SelectKeyPrompt[string] {
	return prompts.SelectKeyModel(prompts.SelectKeyParams[string]{
		Message: message,
		Options: []prompts.SelectKeyOption[string]{
			{Key: "f", Label: "Foo"},
			{Key: "b", Label: "Bar"},
			{Key: "Enter", Label: "Baz"},
		},
	})
}

func TestSelectKeyInitialState(t *testing.T) {
	p := newSelectKeyModel()
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectKeyCancelState(t *testing.T) {
	p := newSelectKeyModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectKeySubmitState(t *testing.T) {
	p := newSelectKeyModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectKeyProgramChoosesShortcut(t *testing.T) {
	p := newSelectKeyModel()
	tm := startTestPrompt(t, p, message)
	tm.Type("b")
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "b", p.Value)
}
