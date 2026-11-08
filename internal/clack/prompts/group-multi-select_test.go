package prompts_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bradleyjkemp/cupaloy"
	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func newGroupMultiSelectModel() *core.GroupMultiSelectPrompt[string] {
	return prompts.GroupMultiSelectModel(prompts.GroupMultiSelectParams[string]{
		Message: message,
		Options: map[string][]prompts.MultiSelectOption[string]{
			"1": {
				{Label: "a"},
				{Label: "b"},
				{Label: "c"},
			},
			"2": {
				{Label: "x"},
				{Label: "y"},
				{Label: "z"},
			},
		},
	})
}

func TestGroupMultiSelectInitialState(t *testing.T) {
	p := newGroupMultiSelectModel()
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectCancelState(t *testing.T) {
	p := newGroupMultiSelectModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectSubmitState(t *testing.T) {
	p := newGroupMultiSelectModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectWithLongList(t *testing.T) {
	p := prompts.GroupMultiSelectModel(prompts.GroupMultiSelectParams[string]{
		Message: message,
		Options: map[string][]prompts.MultiSelectOption[string]{
			"1": {
				{Label: "a"},
				{Label: "b"},
				{Label: "c"},
				{Label: "d"},
				{Label: "e"},
				{Label: "f"},
			},
			"2": {
				{Label: "u"},
				{Label: "v"},
				{Label: "w"},
				{Label: "x"},
				{Label: "y"},
				{Label: "z"},
			},
		},
	})
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectMultiValue(t *testing.T) {
	p := newGroupMultiSelectModel()

	p.Init()
	p.PressKey(&core.Key{Name: core.SpaceKey})
	p.CursorIndex = 5
	p.PressKey(&core.Key{Name: core.SpaceKey})
	p.PressKey(&core.Key{Name: core.DownKey})
	p.PressKey(&core.Key{Name: core.SpaceKey})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectDisabledGroups(t *testing.T) {
	p := prompts.GroupMultiSelectModel(prompts.GroupMultiSelectParams[string]{
		DisabledGroups: true,
		Message:        message,
		Options: map[string][]prompts.MultiSelectOption[string]{
			"1": {
				{Label: "a"},
				{Label: "b"},
			},
			"2": {
				{Label: "x"},
				{Label: "y"},
			},
		},
	})
	p.Init()

	p.PressKey(&core.Key{Name: core.SpaceKey})
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectSpacedGroups(t *testing.T) {
	p := prompts.GroupMultiSelectModel(prompts.GroupMultiSelectParams[string]{
		SpacedGroups: true,
		Message:      message,
		Options: map[string][]prompts.MultiSelectOption[string]{
			"1": {
				{Label: "a"},
				{Label: "b"},
			},
			"2": {
				{Label: "x"},
				{Label: "y"},
			},
		},
	})
	p.Init()

	p.PressKey(&core.Key{Name: core.SpaceKey})
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectDisabledAndSpacedGroups(t *testing.T) {
	p := prompts.GroupMultiSelectModel(prompts.GroupMultiSelectParams[string]{
		DisabledGroups: true,
		SpacedGroups:   true,
		Message:        message,
		Options: map[string][]prompts.MultiSelectOption[string]{
			"1": {
				{Label: "a"},
				{Label: "b"},
			},
			"2": {
				{Label: "x"},
				{Label: "y"},
			},
		},
	})
	p.Init()

	p.PressKey(&core.Key{Name: core.SpaceKey})
	cupaloy.SnapshotT(t, p.Frame)
}

func TestGroupMultiSelectProgramSelectsWholeGroup(t *testing.T) {
	p := newGroupMultiSelectModel()
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, []string{"a", "b", "c"}, p.Value)
}
