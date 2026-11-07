package prompts

import (
	"context"
	"os"
	"strings"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/go-cli-template/internal/clack/prompts/theme"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
)

// ConfirmParams configures a confirmation prompt.
type ConfirmParams struct {
	Context      context.Context
	Input        *os.File
	Output       *os.File
	Message      string
	InitialValue bool
	Active       string
	Inactive     string
}

// Confirm displays a confirmation prompt to the user.
//
// The prompt displays a message and two options: an active and an inactive option.
// The user can toggle between the two options using arrow keys.
// The prompt returns the selected value if the user confirms their choice.
// If the user cancels the prompt, it returns an error.
// If an error occurs during the prompt, it also returns an error.
//
// Parameters:
//   - Context (context.Context): The context for the prompt (default: context.Background).
//   - Input (*os.File): The input stream for the prompt (default: OSFileSystem).
//   - Output (*os.File): The output stream for the prompt (default: OSFileSystem).
//   - Message (string): The message to display to the user (default: "").
//   - InitialValue (bool): The initial value of the prompt (default: false).
//   - Active (string): The active option to display (default: "yes").
//   - Inactive (string): The inactive option to display (default: "no").
//
// Returns:
//   - bool: The selected value if the user confirms their choice.
//   - error: An error if the user cancels the prompt or if an error occurs.

// ConfirmModel constructs a composable Bubble Tea model using the exact
// go-clack themed renderer used by the standalone Confirm prompt.
func ConfirmModel(params ConfirmParams) *core.ConfirmPrompt {
	p := core.NewConfirmPrompt(core.ConfirmPromptParams{
		Context:      params.Context,
		Input:        params.Input,
		Output:       params.Output,
		InitialValue: params.InitialValue,
		Active:       params.Active,
		Inactive:     params.Inactive,
		Render: func(p *core.ConfirmPrompt) string {
			activeRadio := picocolors.Green(symbols.RadioActive)
			inactiveRadio := picocolors.Dim(symbols.RadioInactive)
			slash := picocolors.Dim("/")

			var value, valueWithCursor string
			if p.Value {
				value = p.Active
				valueWithCursor = strings.Join([]string{activeRadio, p.Active, slash, inactiveRadio, picocolors.Dim(p.Inactive)}, " ")
			} else {
				value = p.Inactive
				valueWithCursor = strings.Join([]string{inactiveRadio, picocolors.Dim(p.Active), slash, activeRadio, p.Inactive}, " ")
			}

			return theme.ApplyTheme(theme.Params[bool]{
				Context:         p.Prompt,
				Message:         params.Message,
				Value:           value,
				ValueWithCursor: valueWithCursor,
			})
		},
	})
	return p
}

// Confirm runs the styled model as a standalone Bubble Tea program.
func Confirm(params ConfirmParams) (bool, error) {
	return ConfirmModel(params).Run()
}
