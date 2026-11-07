package prompts_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bradleyjkemp/cupaloy"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func newSelectModel() *core.SelectPrompt[string] {
	return prompts.SelectModel(prompts.SelectParams[string]{
		Message: message,
		Options: []*prompts.SelectOption[string]{
			{Label: "foo"},
			{Label: "bar"},
			{Label: "baz"},
		},
	})
}

func TestSelectInitialState(t *testing.T) {
	p := newSelectModel()
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectWithHint(t *testing.T) {
	p := prompts.SelectModel(prompts.SelectParams[string]{
		Message: message,
		Options: []*prompts.SelectOption[string]{
			{Label: "foo", Hint: "hint-foo"},
			{Label: "bar", Hint: "hint-bar"},
			{Label: "baz", Hint: "hint-baz"},
		},
	})
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectCancelState(t *testing.T) {
	p := newSelectModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectSubmitState(t *testing.T) {
	p := newSelectModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectWithLongList(t *testing.T) {
	p := prompts.SelectModel(prompts.SelectParams[string]{
		Message: message,
		Options: []*prompts.SelectOption[string]{
			{Label: "a"},
			{Label: "b"},
			{Label: "c"},
			{Label: "d"},
			{Label: "e"},
			{Label: "f"},
			{Label: "g"},
			{Label: "h"},
			{Label: "i"},
			{Label: "j"},
			{Label: "k"},
			{Label: "l"},
		},
	})
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectEmptyFilter(t *testing.T) {
	p := prompts.SelectModel(prompts.SelectParams[string]{
		Message: message,
		Filter:  true,
		Options: []*prompts.SelectOption[string]{
			{Label: "foo"},
			{Label: "bar"},
			{Label: "baz"},
		},
	})

	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectFilledFilter(t *testing.T) {
	p := prompts.SelectModel(prompts.SelectParams[string]{
		Message: message,
		Filter:  true,
		Options: []*prompts.SelectOption[string]{
			{Label: "foo"},
			{Label: "bar"},
			{Label: "baz"},
		},
	})

	p.Init()
	p.PressKey(&core.Key{Char: "b"})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectProgramNavigatesAndSubmits(t *testing.T) {
	p := newSelectModel()
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "bar", p.Value)
}

func TestSelectProgramFiltersAndSubmits(t *testing.T) {
	p := prompts.SelectModel(prompts.SelectParams[string]{
		Message: message,
		Filter:  true,
		Options: []*prompts.SelectOption[string]{
			{Label: "foo"}, {Label: "bar"}, {Label: "baz"},
		},
	})
	tm := startTestPrompt(t, p, message)
	tm.Type("bar")
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "bar", p.Value)
	assert.Equal(t, "bar", p.Search)
}
