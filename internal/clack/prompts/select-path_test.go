package prompts_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bradleyjkemp/cupaloy"
	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func newSelectPathModel() *core.SelectPathPrompt {
	return prompts.SelectPathModel(prompts.SelectPathParams{
		Message:    message,
		FileSystem: (prompts.FileSystem)(MockFileSystem{}),
	})
}

func TestSelectPathInitialState(t *testing.T) {
	p := newSelectPathModel()
	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectPathWithOptionChildren(t *testing.T) {
	p := newSelectPathModel()
	p.Init()

	p.PressKey(&core.Key{Name: core.RightKey})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectPathCancelState(t *testing.T) {
	p := newSelectPathModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: 'c', Mod: tea.ModCtrl})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.CancelState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectPathSubmitState(t *testing.T) {
	p := newSelectPathModel()

	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})

	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectPathEmptyFilter(t *testing.T) {
	p := prompts.SelectPathModel(prompts.SelectPathParams{
		Message:    message,
		FileSystem: (prompts.FileSystem)(MockFileSystem{}),
		Filter:     true,
	})

	p.Init()

	assert.Equal(t, core.InitialState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectPathFilledFilter(t *testing.T) {
	p := prompts.SelectPathModel(prompts.SelectPathParams{
		Message:    message,
		FileSystem: (prompts.FileSystem)(MockFileSystem{}),
		Filter:     true,
	})

	p.Init()
	p.PressKey(&core.Key{Char: "f"})

	assert.Equal(t, core.ActiveState, p.State)
	cupaloy.SnapshotT(t, p.Frame)
}

func TestSelectPathProgramFiltersAndSubmits(t *testing.T) {
	p := prompts.SelectPathModel(prompts.SelectPathParams{
		Message: message, Filter: true, FileSystem: MockFileSystem{},
	})
	tm := startTestPrompt(t, p, message)
	tm.Type("file")
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "file", p.Search)
	assert.NotEmpty(t, p.Value)
}

func TestSelectPathProgramNavigatesToFile(t *testing.T) {
	p := newSelectPathModel()
	tm := startTestPrompt(t, p, message)
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, "/clack/file", p.Value)
}
