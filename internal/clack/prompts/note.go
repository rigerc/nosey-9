package prompts

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/core/utils"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
)

// NoteOptions configures a rendered note.
type NoteOptions struct {
	Title  string
	Output io.Writer
}

// Note displays a formatted note box with a title, message, and borders.
func Note(msg string, options NoteOptions) {
	if options.Output == nil {
		options.Output = os.Stdout
	}

	lineLength := utils.StrLength(options.Title) + 7
	for _, line := range utils.SplitLines(msg) {
		lineLength = max(utils.StrLength(line)+4, lineLength)
	}

	frame := strings.Join([]string{
		picocolors.Gray(symbols.Bar),
		noteHeader(options.Title, lineLength),
		noteBody(msg, lineLength),
		noteFooter(lineLength),
		"",
	}, "\r\n")

	_ = core.WriteStyled(options.Output, frame)
}

func noteHeader(title string, lineLength int) string {
	if title == "" {
		left := symbols.ConnectLeft
		top := strings.Repeat(symbols.BarH, lineLength)
		right := symbols.CornerTopRight
		return picocolors.Gray(fmt.Sprint(left, top, right))
	}

	left := picocolors.Green(symbols.StepSubmit)
	topLength := max(lineLength-utils.StrLength(title)-2, 0)
	top := picocolors.Gray(strings.Repeat(symbols.BarH, topLength))
	right := picocolors.Gray(symbols.CornerTopRight)
	return fmt.Sprintf("%s %s %s%s", left, title, top, right)
}

func noteBody(msg string, lineLength int) string {
	bar := picocolors.Gray(symbols.Bar)

	lines := utils.SplitLines("\r\n" + msg + "\r\n")
	body := make([]string, len(lines))

	for i, line := range lines {
		whitespace := strings.Repeat(" ", max(lineLength-2-utils.StrLength(line), 1))
		body[i] = fmt.Sprintf("%s  %s%s%s", bar, line, whitespace, bar)
	}

	return strings.Join(body, "\r\n")
}

func noteFooter(lineLength int) string {
	left := symbols.ConnectLeft
	bottom := strings.Repeat(symbols.BarH, lineLength)
	right := symbols.CornerBottomRight

	return picocolors.Gray(fmt.Sprint(left, bottom, right))
}
