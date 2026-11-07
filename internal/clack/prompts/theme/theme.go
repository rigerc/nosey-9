// Package theme renders the original go-clack prompt frames.
package theme

import (
	"strings"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/core/utils"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
)

// Value constrains values accepted by ApplyTheme.
type Value interface {
	string | any | []any
}

// Params holds the data needed to render a prompt frame.
type Params[TValue Value] struct {
	Context         core.Prompt[TValue]
	Message         string
	Value           string
	ValueWithCursor string
	Placeholder     string
}

// ApplyTheme renders a prompt frame with the go-clack theme.
func ApplyTheme[TValue Value](params Params[TValue]) string {
	ctx := params.Context

	frame := make([]string, 0, 4)
	frame = append(frame, picocolors.Gray(symbols.Bar))

	symbolColor := SymbolColor(ctx.State)
	barColor := BarColor(ctx.State)
	title := ctx.FormatLines(utils.SplitLines(params.Message), core.FormatLinesOptions{
		FirstLine: core.FormatLineOptions{
			Start: symbolColor(symbols.State(ctx.State)),
		},
		NewLine: core.FormatLineOptions{
			Start: barColor(symbols.Bar),
		},
	})
	frame = append(frame, title)

	var valueWithCursor string
	if (params.ValueWithCursor == "█" || params.ValueWithCursor == "") && params.Placeholder != "" {
		valueWithCursor = picocolors.Inverse(string(params.Placeholder[0])) + picocolors.Dim(params.Placeholder[1:])
	} else {
		valueWithCursor = params.ValueWithCursor
	}

	switch ctx.State {
	case core.ErrorState:
		value := ctx.FormatLines(utils.SplitLines(valueWithCursor), core.FormatLinesOptions{
			Default: core.FormatLineOptions{
				Start: barColor(symbols.Bar),
			},
		})
		frame = append(frame, value)

		if ctx.Error != "" {
			err := ctx.FormatLines(utils.SplitLines(ctx.Error), core.FormatLinesOptions{
				Default: core.FormatLineOptions{
					Start: barColor(symbols.Bar),
					Style: picocolors.Yellow,
				},
				LastLine: core.FormatLineOptions{
					Start: barColor(symbols.BarEnd),
				},
			})
			frame = append(frame, err)
		}

	case core.CancelState:
		value := ctx.FormatLines(utils.SplitLines(params.Value), core.FormatLinesOptions{
			Default: core.FormatLineOptions{
				Start: barColor(symbols.Bar),
				Style: func(line string) string {
					return picocolors.Strikethrough(picocolors.Dim(line))
				},
			},
		})
		frame = append(frame, value)

		if params.Value != "" {
			end := barColor(symbols.Bar)
			frame = append(frame, end)
		}

	case core.SubmitState:
		value := ctx.FormatLines(utils.SplitLines(params.Value), core.FormatLinesOptions{
			Default: core.FormatLineOptions{
				Start: barColor(symbols.Bar),
				Style: picocolors.Dim,
			},
		})
		frame = append(frame, value)

	case core.ValidateState:
		value := ctx.FormatLines(utils.SplitLines(params.Value), core.FormatLinesOptions{
			Default: core.FormatLineOptions{
				Start: barColor(symbols.Bar),
				Style: picocolors.Dim,
			},
		})
		dots := strings.Repeat(".", int(ctx.ValidationDuration.Seconds())%4)
		validatingMsg := barColor(symbols.BarEnd) + " " + picocolors.Dim("validating"+dots)
		frame = append(frame, value, validatingMsg)

	default:
		value := ctx.FormatLines(utils.SplitLines(valueWithCursor), core.FormatLinesOptions{
			Default: core.FormatLineOptions{
				Start: barColor(symbols.Bar),
			},
		})
		end := barColor(symbols.BarEnd)
		frame = append(frame, value, end)
	}

	return strings.Join(frame, "\r\n")
}

// SymbolColor returns the state symbol color.
func SymbolColor(state core.State) func(input string) string {
	switch state {
	case core.ErrorState, core.CancelState:
		return picocolors.Red
	case core.SubmitState:
		return picocolors.Green
	default:
		return picocolors.Cyan
	}
}

// BarColor returns the state bar color.
func BarColor(state core.State) func(input string) string {
	switch state {
	case core.ErrorState:
		return picocolors.Yellow
	case core.InitialState, core.ActiveState:
		return picocolors.Cyan
	default:
		return picocolors.Gray
	}
}
