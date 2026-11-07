package prompts

import (
	"context"
	"os"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/theme"
)

// PasswordParams configures a password prompt.
type PasswordParams struct {
	Context      context.Context
	Input        *os.File
	Output       *os.File
	Message      string
	InitialValue string
	Required     bool
	Validate     func(value string) error
}

// Password displays a password input prompt to the user.
//
// The prompt displays a message.
// The user can input a password.
// The password is masked by asterisks ("*").
// The prompt returns the password without the mask.
// If the user cancels the prompt, it returns an error.
// If an error occurs during the prompt, it also returns an error.
//
// Parameters:
//   - Context (context.Context): The context for the prompt (default: context.Background).
//   - Input (*os.File): The input stream for the prompt (default: OSFileSystem).
//   - Output (*os.File): The output stream for the prompt (default: OSFileSystem).
//   - Message (string): The message to display to the user (default: "").
//   - InitialValue (string): The initial value of the password input (default: "").
//   - Required (bool): Whether the password input is required (default: false).
//   - Validate (func(value string) error): Custom validation function for the password (default: nil).
//
// Returns:
//   - string: The password without the mask.
//   - error: An error if the user cancels the prompt or if an error occurs.

// PasswordModel constructs a composable Bubble Tea model using the exact
// go-clack themed renderer used by the standalone Password prompt.
func PasswordModel(params PasswordParams) *core.PasswordPrompt {
	p := core.NewPasswordPrompt(core.PasswordPromptParams{
		Context:      params.Context,
		Input:        params.Input,
		Output:       params.Output,
		InitialValue: params.InitialValue,
		Required:     params.Required,
		Validate:     params.Validate,
		Render: func(p *core.PasswordPrompt) string {
			return theme.ApplyTheme(theme.Params[string]{
				Context:         p.Prompt,
				Message:         params.Message,
				Value:           p.ValueWithMask(),
				ValueWithCursor: p.ValueWithMaskAndCursor(),
			})
		},
	})
	return p
}

// Password runs the styled model as a standalone Bubble Tea program.
func Password(params PasswordParams) (string, error) {
	return PasswordModel(params).Run()
}
