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

// MultiSelectOption is one selectable multi-select item.
type MultiSelectOption[TValue comparable] struct {
	Label      string
	Value      TValue
	Hint       string
	IsSelected bool
}

// MultiSelectParams configures a multi-select prompt.
type MultiSelectParams[TValue comparable] struct {
	Context      context.Context
	Input        *os.File
	Output       *os.File
	Message      string
	Options      []*MultiSelectOption[TValue]
	InitialValue []TValue
	Filter       bool
	Required     bool
	Validate     func(value []TValue) error
}

// MultiSelect displays a multi-select prompt to the user.
//
// The prompt displays a message within their options.
// The user can navigate through options using arrow keys.
// The user can select multiple options using space key.
// The prompt returns the value of the selected options.
// If the user cancels the prompt, it returns an error.
// If an error occurs during the prompt, it also returns an error.
//
// Parameters:
//   - Context (context.Context): The context for the prompt (default: context.Background).
//   - Input (*os.File): The input stream for the prompt (default: OSFileSystem).
//   - Output (*os.File): The output stream for the prompt (default: OSFileSystem).
//   - Message (string): The message to display to the user (default: "").
//   - Options ([]*MultiSelectOption[TValue]): A list of options for the prompt (default: nil.
//   - InitialValue ([]TValue): The initial selected values (default: nil.
//   - Filter (bool): Whether to enable filtering of options (default: false).
//   - Required (bool): Whether the prompt requires at least one selection (default: false).
//   - Validate (func(value []TValue) error): Custom validation function for the prompt (default: nil).
//
// Returns:
//   - []TValue: A slice of values of the selected options.
//   - error: An error if the user cancels the prompt or if an error occurs.

// MultiSelectModel constructs a composable Bubble Tea model using the exact
// go-clack themed renderer used by the standalone MultiSelect prompt.
func MultiSelectModel[TValue comparable](params MultiSelectParams[TValue]) *core.MultiSelectPrompt[TValue] {
	v := validator.NewValidator("MultiSelect")
	v.ValidateOptions(len(params.Options))

	options := make([]*core.MultiSelectOption[TValue], len(params.Options))
	for i, option := range params.Options {
		options[i] = &core.MultiSelectOption[TValue]{
			Label:      option.Label,
			Value:      option.Value,
			Hint:       option.Hint,
			IsSelected: option.IsSelected,
		}
	}

	p := core.NewMultiSelectPrompt(core.MultiSelectPromptParams[TValue]{
		Context:      params.Context,
		Input:        params.Input,
		Output:       params.Output,
		InitialValue: params.InitialValue,
		Options:      options,
		Filter:       params.Filter,
		Required:     params.Required,
		Validate:     params.Validate,
		Render: func(p *core.MultiSelectPrompt[TValue]) string {
			message := params.Message
			var value string

			switch p.State {
			case core.SubmitState, core.CancelState:
				for _, option := range p.Options {
					if option.IsSelected {
						if value == "" {
							value = option.Label
						} else {
							value += ", " + option.Label
						}
					}
				}

			default:
				radioOptions := make([]string, len(p.Options))
				for i, option := range p.Options {
					var radio, label, hint string
					switch {
					case option.IsSelected && i == p.CursorIndex:
						radio = picocolors.Green(symbols.CheckboxSelected)
						label = option.Label
						if option.Hint != "" {
							hint = picocolors.Dim("(" + option.Hint + ")")
						}
					case i == p.CursorIndex:
						radio = picocolors.Green(symbols.CheckboxActive)
						label = option.Label
						if option.Hint != "" {
							hint = picocolors.Dim("(" + option.Hint + ")")
						}
					case option.IsSelected:
						radio = picocolors.Green(symbols.CheckboxSelected)
						label = picocolors.Dim(option.Label)
						if option.Hint != "" {
							hint = picocolors.Dim("(" + option.Hint + ")")
						}
					default:
						radio = picocolors.Dim(symbols.CheckboxInactive)
						label = picocolors.Dim(option.Label)
					}
					parts := []string{radio, label}
					if hint != "" {
						parts = append(parts, hint)
					}
					radioOptions[i] = strings.Join(parts, " ")
				}

				if p.Filter {
					if p.Search == "" {
						message = fmt.Sprintf("%s\n> %s", message, picocolors.Inverse("T")+picocolors.Dim("ype to filter..."))
					} else {
						message = fmt.Sprintf("%s\n> %s", message, p.Search+"█")
					}

					value = p.LimitLines(radioOptions, 4)
					break
				}

				value = p.LimitLines(radioOptions, 3)
			}

			return theme.ApplyTheme(theme.Params[[]TValue]{
				Context:         p.Prompt,
				Message:         message,
				Value:           value,
				ValueWithCursor: value,
			})
		},
	})
	return p
}

// MultiSelect runs the styled model as a standalone Bubble Tea program.
func MultiSelect[TValue comparable](params MultiSelectParams[TValue]) ([]TValue, error) {
	return MultiSelectModel(params).Run()
}
