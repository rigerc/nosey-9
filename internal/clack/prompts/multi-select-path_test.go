package prompts_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bradleyjkemp/cupaloy"
	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func newMultiSelectPathModel() *core.MultiSelectPathPrompt {
	return prompts.MultiSelectPathModel(prompts.MultiSelectPathParams{
		Message:    message,
		FileSystem: (prompts.FileSystem)(MockFileSystem{}),
	})
}

func TestMultiSelectPathInitialState(t *testing.T) {
	p := newMultiSelectPathModel()
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathWithOptionChildren(t *testing.T) {
	p := newMultiSelectPathModel()
	p.Init()

	p.PressKey(&core.Key{Name: core.RightKey})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathWithSelectedOptions(t *testing.T) {
	p := newMultiSelectPathModel()
	p.Init()

	p.PressKey(&core.Key{Name: core.SpaceKey})
	p.PressKey(&core.Key{Name: core.DownKey})
	p.PressKey(&core.Key{Name: core.SpaceKey})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathCancelState(t *testing.T) {
	p := newMultiSelectPathModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathSubmitState(t *testing.T) {
	p := newMultiSelectPathModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathEmptyFilter(t *testing.T) {
	p := prompts.MultiSelectPathModel(prompts.MultiSelectPathParams{
		Message:    message,
		FileSystem: (prompts.FileSystem)(MockFileSystem{}),
		Filter:     true,
	})

	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathFilledFilter(t *testing.T) {
	p := prompts.MultiSelectPathModel(prompts.MultiSelectPathParams{
		Message:    message,
		FileSystem: (prompts.FileSystem)(MockFileSystem{}),
		Filter:     true,
	})

	p.Init()
	p.PressKey(&core.Key{Char: "f"})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestMultiSelectPathProgramSelectsMultipleFiles(t *testing.T) {
	p := newMultiSelectPathModel()
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Len(t, p.Value, 2)
}

func TestMultiSelectPathProgramFiltersAndSubmits(t *testing.T) {
	p := prompts.MultiSelectPathModel(prompts.MultiSelectPathParams{
		Message:    message,
		Filter:     true,
		FileSystem: MockFileSystem{},
	})
	tm := startTestPrompt(t, p, message)
	tm.Type("file")
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, []string{"/clack/file"}, p.Value)
}
