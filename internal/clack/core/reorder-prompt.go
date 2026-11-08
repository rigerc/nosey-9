package core

import (
	"context"
	"os"

	"github.com/rigerc/nosey-9/internal/clack/core/validator"
)

// ReorderOption is an item that can be moved in a ReorderPrompt.
type ReorderOption[T comparable] struct {
	Label string
	Value T
	Hint  string
}

// ReorderPrompt lets users change the order of a list.
// Space picks up or drops the current item; arrows move it while picked up.
type ReorderPrompt[T comparable] struct {
	Prompt[[]T]
	Options []*ReorderOption[T]
	Picked  bool
}

// ReorderPromptParams configures a reorder prompt.
type ReorderPromptParams[T comparable] struct {
	Context context.Context
	Input   *os.File
	Output  *os.File
	Options []*ReorderOption[T]
	Render  func(*ReorderPrompt[T]) string
}

// NewReorderPrompt creates a prompt that returns its items in the submitted order.
// The caller's options are copied; editing the prompt does not mutate the input.
func NewReorderPrompt[T comparable](params ReorderPromptParams[T]) *ReorderPrompt[T] {
	v := validator.NewValidator("ReorderPrompt")
	v.ValidateRender(params.Render)
	v.ValidateOptions(len(params.Options))

	options := make([]*ReorderOption[T], len(params.Options))
	values := make([]T, len(params.Options))
	for i, option := range params.Options {
		if option == nil {
			v.Panic("Options must not contain nil items")
		}
		copyOption := *option
		if value, ok := any(copyOption.Value).(string); ok && value == "" {
			if label, ok := any(copyOption.Label).(T); ok {
				copyOption.Value = label
			}
		}
		options[i] = &copyOption
		values[i] = copyOption.Value
	}

	var p ReorderPrompt[T]
	p = ReorderPrompt[T]{
		Prompt: *NewPrompt(PromptParams[[]T]{
			Context:      params.Context,
			Input:        params.Input,
			Output:       params.Output,
			InitialValue: values,
			Render:       WrapRender[[]T](&p, params.Render),
		}),
		Options: options,
	}

	p.HandleKey = func(key *Key) {
		if p.State == SubmitState || p.State == CancelState {
			return
		}
		switch key.Name {
		case SpaceKey:
			p.Picked = !p.Picked
		case UpKey, LeftKey:
			p.move(-1)
		case DownKey, RightKey:
			p.move(1)
		case HomeKey:
			p.goTo(0)
		case EndKey:
			p.goTo(len(p.Options) - 1)
		}
	}
	return &p
}

func (p *ReorderPrompt[T]) move(delta int) {
	next := p.CursorIndex + delta
	if !p.Picked {
		p.CursorIndex = (next + len(p.Options)) % len(p.Options)
		return
	}
	if next < 0 || next >= len(p.Options) {
		return
	}
	p.Options[p.CursorIndex], p.Options[next] = p.Options[next], p.Options[p.CursorIndex]
	p.Value[p.CursorIndex], p.Value[next] = p.Value[next], p.Value[p.CursorIndex]
	p.CursorIndex = next
}

func (p *ReorderPrompt[T]) goTo(index int) {
	if !p.Picked {
		p.CursorIndex = index
		return
	}
	for p.CursorIndex != index {
		if index < p.CursorIndex {
			p.move(-1)
		} else {
			p.move(1)
		}
	}
}
