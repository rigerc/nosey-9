package prompts

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/core/validator"
	"github.com/rigerc/nosey-9/internal/clack/prompts/symbols"
	"github.com/rigerc/nosey-9/internal/clack/prompts/theme"
	"github.com/rigerc/nosey-9/internal/clack/third_party/picocolors"
)

// ReorderOption is one item in a reorder prompt.
type ReorderOption[T comparable] struct {
	Label string
	Value T
	Hint  string
}

// ReorderParams configures a reorder prompt.
type ReorderParams[T comparable] struct {
	Context context.Context
	Input   *os.File
	Output  *os.File
	Message string
	Options []*ReorderOption[T]
}

// Reorder displays an interactive ordered list. Use arrows to select an item,
// Space to pick it up or drop it, and arrows to move it while picked up.
// Enter submits all item values in their final order; Ctrl+C cancels.

// ReorderModel constructs a composable Bubble Tea model using the exact
// go-clack themed renderer used by the standalone Reorder prompt.
func ReorderModel[T comparable](params ReorderParams[T]) *core.ReorderPrompt[T] {
	v := validator.NewValidator("Reorder")
	v.ValidateOptions(len(params.Options))
	options := make([]*core.ReorderOption[T], len(params.Options))
	for i, option := range params.Options {
		if option == nil {
			v.Panic("Options must not contain nil items")
		}
		options[i] = &core.ReorderOption[T]{
			Label: option.Label,
			Value: option.Value,
			Hint:  option.Hint,
		}
	}

	p := core.NewReorderPrompt(core.ReorderPromptParams[T]{
		Context: params.Context,
		Input:   params.Input,
		Output:  params.Output,
		Options: options,
		Render: func(p *core.ReorderPrompt[T]) string {
			var value string
			switch p.State {
			case core.SubmitState, core.CancelState, core.ValidateState:
				labels := make([]string, len(p.Options))
				for i, option := range p.Options {
					labels[i] = option.Label
				}
				value = strings.Join(labels, " → ")
			default:
				lines := make([]string, len(p.Options))
				for i, option := range p.Options {
					marker := picocolors.Dim(symbols.RadioInactive)
					label := picocolors.Dim(option.Label)
					if i == p.CursorIndex {
						marker = picocolors.Green(symbols.RadioActive)
						if p.Picked {
							marker = picocolors.Yellow("↕")
						}
						label = option.Label
					}
					hint := ""
					if option.Hint != "" && i == p.CursorIndex {
						hint = " " + picocolors.Dim(fmt.Sprintf("(%s)", option.Hint))
					}
					lines[i] = fmt.Sprintf("%s %s%s", marker, label, hint)
				}
				instructions := picocolors.Dim("Space: pick up/drop • ↑/↓: navigate/move • Enter: confirm")
				value = p.LimitLines(lines, 4) + "\r\n" + instructions
			}

			return theme.ApplyTheme(theme.Params[[]T]{
				Context:         p.Prompt,
				Message:         params.Message,
				Value:           value,
				ValueWithCursor: value,
			})
		},
	})
	return p
}

// Reorder runs the styled model as a standalone Bubble Tea program.
func Reorder[T comparable](params ReorderParams[T]) ([]T, error) {
	return ReorderModel(params).Run()
}
