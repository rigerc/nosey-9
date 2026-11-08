package prompts

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts/symbols"
	"github.com/rigerc/nosey-9/internal/clack/prompts/theme"
	"github.com/rigerc/nosey-9/internal/clack/third_party/picocolors"
)

// MultiSelectPathParams configures a multi-select path prompt.
type MultiSelectPathParams struct {
	Context      context.Context
	Input        *os.File
	Output       *os.File
	Message      string
	InitialValue []string
	InitialPath  string
	Required     bool
	OnlyShowDir  bool
	Filter       bool
	FileSystem   FileSystem
	Validate     func(value []string) error
}

// MultiSelectPath displays a multi-select prompt to the user.
//
// The prompt displays a message within their options.
// The user can navigate through directories and files using arrow keys.
// The user can select multiple options using space key.
// The prompt returns the path of the selected options.
// If the user cancels the prompt, it returns an error.
// If an error occurs during the prompt, it also returns an error.
//
// Parameters:
//   - Context (context.Context): The context for the prompt (default: context.Background).
//   - Input (*os.File): The input stream for the prompt (default: OSFileSystem).
//   - Output (*os.File): The output stream for the prompt (default: OSFileSystem).
//   - Message (string): The message to display to the user (default: "").
//   - InitialValue ([]string): Initial selected paths (default: nil).
//   - InitialPath (string): The initial directory path to start from (default: current working directory).
//   - OnlyShowDir (bool): Whether to only show directories (default: false).
//   - Required (bool): Whether at least one option must be selected (default: false).
//   - Filter (bool): Whether to enable filtering of options (default: false).
//   - FileSystem (FileSystem): The file system implementation to use (default: OSFileSystem).
//   - Validate (func(value []TValue) error): Custom validation function for the prompt (default: nil).
//
// Returns:
//   - []string: A slice of paths of the selected options.
//   - error: An error if the user cancels the prompt or if an error occurs.

// MultiSelectPathModel constructs a composable Bubble Tea model using the exact
// go-clack themed renderer used by the standalone MultiSelectPath prompt.
func MultiSelectPathModel(params MultiSelectPathParams) *core.MultiSelectPathPrompt {
	p := core.NewMultiSelectPathPrompt(core.MultiSelectPathPromptParams{
		Context:      params.Context,
		Input:        params.Input,
		Output:       params.Output,
		InitialValue: params.InitialValue,
		InitialPath:  params.InitialPath,
		OnlyShowDir:  params.OnlyShowDir,
		FileSystem:   params.FileSystem,
		Required:     params.Required,
		Filter:       params.Filter,
		Validate:     params.Validate,
		Render: func(p *core.MultiSelectPathPrompt) string {
			message := params.Message
			var value string

			switch p.State {
			case core.SubmitState, core.CancelState:
			default:
				options := p.Options()
				radioOptions := make([]string, len(options))
				for i, option := range options {
					var radio, label, dir string
					switch {
					case option.IsDir && option.IsOpen:
						dir = "v"
					case option.IsDir:
						dir = ">"
					}
					switch {
					case option.IsSelected && option.IsEqual(p.CurrentOption):
						radio = picocolors.Green(symbols.CheckboxSelected)
						label = option.Name
					case option.IsSelected:
						radio = picocolors.Green(symbols.CheckboxSelected)
						label = picocolors.Dim(option.Name)
						dir = picocolors.Dim(dir)
					case option.IsEqual(p.CurrentOption):
						radio = picocolors.Green(symbols.CheckboxActive)
						label = option.Name
					default:
						radio = picocolors.Dim(symbols.CheckboxInactive)
						label = picocolors.Dim(option.Name)
						dir = picocolors.Dim(dir)
					}
					depth := strings.Repeat(" ", option.Depth)
					radioOptions[i] = fmt.Sprintf("%s%s %s %s", depth, radio, label, dir)
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

			return theme.ApplyTheme(theme.Params[[]string]{
				Context:         p.Prompt,
				Message:         message,
				Value:           strings.Join(p.Value, "\r\n"),
				ValueWithCursor: value,
			})
		},
	})
	return p
}

// MultiSelectPath runs the styled model as a standalone Bubble Tea program.
func MultiSelectPath(params MultiSelectPathParams) ([]string, error) {
	return MultiSelectPathModel(params).Run()
}
