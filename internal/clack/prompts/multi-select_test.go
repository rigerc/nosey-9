package prompts_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bradleyjkemp/cupaloy"
	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func newMultiSelectModel() *core.MultiSelectPrompt[string] {
	return prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo", IsSelected: true},
			{Label: "bar", IsSelected: true},
			{Label: "baz"},
		},
	})
}

func TestMultiSelectInitialState(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo"},
			{Label: "bar"},
			{Label: "baz"},
		},
	})
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectWithHint(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo", Hint: "hint-foo"},
			{Label: "bar", Hint: "hint-bar"},
			{Label: "baz", Hint: "hint-baz"},
		},
	})
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectWithSelectedHint(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo", Hint: "hint-foo"},
			{Label: "bar", Hint: "hint-bar"},
			{Label: "baz", Hint: "hint-baz"},
		},
	})
	p.Init()

	p.PressKey(&core.Key{Name: core.SpaceKey})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectCancelState(t *testing.T) {
	p := newMultiSelectModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectSubmitState(t *testing.T) {
	p := newMultiSelectModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectWithLongList(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "a"},
			{Label: "b"},
			{Label: "c"},
			{Label: "a"},
			{Label: "b"},
			{Label: "c"},
			{Label: "a"},
			{Label: "b"},
			{Label: "c"},
			{Label: "a"},
			{Label: "b"},
			{Label: "c"},
		},
	})
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectMultiValue(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "a", IsSelected: true},
			{Label: "b", IsSelected: true},
			{Label: "c"},
			{Label: "a", IsSelected: true},
			{Label: "b", IsSelected: true},
			{Label: "c"},
		},
	})
	p.Init()
	p.CursorIndex = 1
	p.PressKey(&core.Key{Name: core.DownKey})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectEmptyFilter(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Filter:  true,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo"},
			{Label: "bar"},
			{Label: "baz"},
		},
	})

	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectFilledFilter(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Filter:  true,
		Options: []*prompts.MultiSelectOption[string]{
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

func TestMultiSelectProgramSelectsMultipleValues(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo"}, {Label: "bar"}, {Label: "baz"},
		},
	})
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, []string{"foo", "bar"}, p.Value)
}

func TestMultiSelectProgramFiltersAndSelects(t *testing.T) {
	p := prompts.MultiSelectModel(prompts.MultiSelectParams[string]{
		Message: message,
		Filter:  true,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "foo"}, {Label: "bar"}, {Label: "baz"},
		},
	})
	tm := startTestPrompt(t, p, message)
	tm.Type("bar")
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, []string{"bar"}, p.Value)
	assert.Equal(t, "bar", p.Search)
}
