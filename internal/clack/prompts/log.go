package prompts

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/core/utils"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
)

// MessageLineOptions configures one line of a message block.
type MessageLineOptions = core.FormatLineOptions

// MessageOptions configures a message block.
type MessageOptions = core.FormatLinesOptions

// Message prints a formatted message block.
func Message(msg string, options MessageOptions) {
	p := &core.Prompt[string]{}
	formattedMsg := p.FormatLines(utils.SplitLines(msg), options)
	_ = core.WriteStyled(os.Stdout, fmt.Sprintf("%s\r\n%s\r\n", picocolors.Gray(symbols.Bar), formattedMsg))
}

func styleMsg(msg string, style func(msg string) string) string {
	parts := utils.SplitLines(msg)
	styledParts := make([]string, len(parts))
	for i, part := range parts {
		styledParts[i] = style(part)
	}
	return strings.Join(styledParts, "\r\n")
}

// Intro displays an introductory message.
func Intro(msg string) {
	p := &core.Prompt[string]{}
	formattedMsg := p.FormatLines(utils.SplitLines(msg), MessageOptions{
		FirstLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.BarStart),
		},
		NewLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
	})
	_ = core.WriteStyled(os.Stdout, fmt.Sprintf("\r\n%s\r\n%s\r\n", formattedMsg, picocolors.Gray(symbols.Bar)))
}

// Cancel displays a cancellation message styled in red.
func Cancel(msg string) {
	Message(styleMsg(msg, picocolors.Red), MessageOptions{
		Default: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
		LastLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.BarEnd),
		},
	})
}

// Outro displays a closing message.
func Outro(msg string) {
	Message("\r\n"+msg, MessageOptions{
		Default: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
		LastLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.BarEnd),
		},
	})
}

// Info displays an informational message with a blue info symbol.
func Info(msg string) {
	Message(msg, MessageOptions{
		FirstLine: MessageLineOptions{
			Start: picocolors.Blue(symbols.Info),
		},
		NewLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
	})
}

// Success displays a success message with a green success symbol.
func Success(msg string) {
	Message(msg, MessageOptions{
		FirstLine: MessageLineOptions{
			Start: picocolors.Green(symbols.Success),
		},
		NewLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
	})
}

// Step displays a step message with a green step symbol.
func Step(msg string) {
	Message(msg, MessageOptions{
		FirstLine: MessageLineOptions{
			Start: picocolors.Green(symbols.StepSubmit),
		},
		NewLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
	})
}

// Warn displays a warning message with a yellow warning symbol.
func Warn(msg string) {
	Message(msg, MessageOptions{
		FirstLine: MessageLineOptions{
			Start: picocolors.Yellow(symbols.Warn),
		},
		NewLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
	})
}

// Error displays an error message with a red error symbol.
func Error(msg string) {
	Message(msg, MessageOptions{
		FirstLine: MessageLineOptions{
			Start: picocolors.Red(symbols.Error),
		},
		NewLine: MessageLineOptions{
			Start: picocolors.Gray(symbols.Bar),
		},
	})
}
