package core_test

import (
	"testing"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/stretchr/testify/assert"
)

func newReorderPrompt() *core.ReorderPrompt[string] {
	return core.NewReorderPrompt(core.ReorderPromptParams[string]{
		Options: []*core.ReorderOption[string]{
			{Label: "A", Value: "a"},
			{Label: "B", Value: "b"},
			{Label: "C", Value: "c"},
		},
		Render: func(*core.ReorderPrompt[string]) string { return "reorder" },
	})
}

func TestReorderNavigationAndMoving(t *testing.T) {
	p := newReorderPrompt()
	p.PressKey(&core.Key{Name: core.DownKey})
	assert.Equal(t, 1, p.CursorIndex)
	assert.Equal(t, []string{"a", "b", "c"}, p.Value)

	p.PressKey(&core.Key{Name: core.SpaceKey})
	assert.True(t, p.Picked)
	p.PressKey(&core.Key{Name: core.DownKey})
	assert.Equal(t, 2, p.CursorIndex)
	assert.Equal(t, []string{"a", "c", "b"}, p.Value)

	// Reaching the boundary while carrying an item does not wrap.
	p.PressKey(&core.Key{Name: core.DownKey})
	assert.Equal(t, 2, p.CursorIndex)
	assert.Equal(t, []string{"a", "c", "b"}, p.Value)

	p.PressKey(&core.Key{Name: core.HomeKey})
	assert.Equal(t, 0, p.CursorIndex)
	assert.Equal(t, []string{"b", "a", "c"}, p.Value)

	p.PressKey(&core.Key{Name: core.SpaceKey})
	assert.False(t, p.Picked)
	p.PressKey(&core.Key{Name: core.UpKey})
	assert.Equal(t, 2, p.CursorIndex)
	assert.Equal(t, []string{"b", "a", "c"}, p.Value)

	p.PressKey(&core.Key{Name: core.EnterKey})
	assert.Equal(t, core.SubmitState, p.State)
	assert.Equal(t, []string{"b", "a", "c"}, p.Value)
}

func TestReorderDoesNotMutateCallerOptions(t *testing.T) {
	input := []*core.ReorderOption[string]{
		{Label: "One"}, {Label: "Two"}, {Label: "Three"},
	}
	p := core.NewReorderPrompt(core.ReorderPromptParams[string]{
		Options: input,
		Render:  func(*core.ReorderPrompt[string]) string { return "reorder" },
	})
	p.PressKey(&core.Key{Name: core.SpaceKey})
	p.PressKey(&core.Key{Name: core.DownKey})
	assert.Equal(t, []string{"Two", "One", "Three"}, p.Value)
	assert.Equal(t, "", input[0].Value)
	assert.Equal(t, "One", input[0].Label)
	assert.Equal(t, "Two", input[1].Label)
}

func TestReorderCancel(t *testing.T) {
	p := newReorderPrompt()
	p.PressKey(&core.Key{Name: core.CancelKey})
	assert.Equal(t, core.CancelState, p.State)
}
