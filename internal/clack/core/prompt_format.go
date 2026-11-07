package core

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/go-cli-template/internal/clack/core/utils"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
)

// TrackKeyValue updates the string value and cursor position based on key presses.
func (p *Prompt[TValue]) TrackKeyValue(key *Key, value string, cursorIndex int) (newValue string, newCursorIndex int) {
	// CursorIndex is a byte offset, but all movements and deletions operate
	// on full UTF-8 code points, never bytes inside a character.
	cursorIndex = max(0, min(cursorIndex, len(value)))
	before, after := value[:cursorIndex], value[cursorIndex:]
	switch key.Name {
	case BackspaceKey:
		if len(before) == 0 {
			return value, cursorIndex
		}
		_, n := utf8.DecodeLastRuneInString(before)
		return before[:len(before)-n] + after, cursorIndex - n
	case HomeKey:
		return value, 0
	case EndKey:
		return value, len(value)
	case LeftKey:
		if len(before) == 0 {
			return value, 0
		}
		_, n := utf8.DecodeLastRuneInString(before)
		return value, cursorIndex - n
	case RightKey:
		if len(after) == 0 {
			return value, len(value)
		}
		_, n := utf8.DecodeRuneInString(after)
		return value, cursorIndex + n
	case SpaceKey:
		return before + " " + after, cursorIndex + 1
	}
	if key.Char != "" {
		return before + key.Char + after, cursorIndex + len(key.Char)
	}
	return value, cursorIndex
}

// LimitLines limits the number of lines to fit within the terminal size.
func (p *Prompt[TValue]) LimitLines(lines []string, usedLines int) string {
	_, maxRows, err := p.Size()
	if err != nil {
		maxRows = 10
	}
	maxItems := max(min(maxRows-usedLines-p.HeaderLines, len(lines)), 0)

	slidingWindowLocation := 0
	if p.CursorIndex >= maxItems-3 {
		slidingWindowLocation = max(min(p.CursorIndex-maxItems+3, len(lines)-maxItems), 0)
	} else if p.CursorIndex < 2 {
		slidingWindowLocation = max(p.CursorIndex-2, 0)
	}

	result := make([]string, 0, maxItems)
	shouldRenderTopEllipsis := maxItems < len(lines) && slidingWindowLocation > 0
	shouldRenderBottomEllipsis := maxItems < len(lines) && slidingWindowLocation+maxItems < len(lines)

	for i, line := range lines[slidingWindowLocation : slidingWindowLocation+maxItems] {
		isTopLimit := i == 0 && shouldRenderTopEllipsis
		isBottomLimit := i == maxItems-1 && shouldRenderBottomEllipsis
		if isTopLimit || isBottomLimit {
			result = append(result, picocolors.Dim("..."))
			continue
		}
		result = append(result, line)
	}

	return strings.Join(result, "\r\n")
}

// LineOption identifies a line position in formatted output.
type LineOption int

// LineOption identifies a line position.
const (
	FirstLine LineOption = iota
	NewLine
	LastLine
)

// FormatLineOptions configures one formatted line.
type FormatLineOptions struct {
	Start string
	End   string
	Sides string
	Style func(line string) string
}

// FormatLinesOptions configures a formatted block.
type FormatLinesOptions struct {
	FirstLine FormatLineOptions
	NewLine   FormatLineOptions
	LastLine  FormatLineOptions
	Default   FormatLineOptions
	MinWidth  int
	MaxWidth  int
}

// getOptionOrDefault retrieves the option for the given line and option type, or returns a default value.
func getOptionOrDefault(line LineOption, option string, options FormatLinesOptions) string {
	var target FormatLineOptions
	switch line {
	case FirstLine:
		target = options.FirstLine
	case NewLine:
		target = options.NewLine
	case LastLine:
		target = options.LastLine
	}

	switch option {
	case "start":
		if value := firstNonEmpty(target.Start, target.Sides, options.Default.Start); value != "" {
			return value
		}
	case "end":
		if value := firstNonEmpty(target.End, target.Sides, options.Default.End); value != "" {
			return value
		}
	}
	if options.Default.Sides != "" {
		return options.Default.Sides
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func getStyleOrDefault(line LineOption, options FormatLinesOptions) func(line string) string {
	switch line {
	case FirstLine:
		if options.FirstLine.Style != nil {
			return options.FirstLine.Style
		}
	case NewLine:
		if options.NewLine.Style != nil {
			return options.NewLine.Style
		}
	case LastLine:
		if options.LastLine.Style != nil {
			return options.LastLine.Style
		}
	}
	if options.Default.Style != nil {
		return options.Default.Style
	}
	return func(line string) string { return line }
}

func getLineOptions(options FormatLinesOptions, line LineOption) FormatLineOptions {
	return FormatLineOptions{
		Start: getOptionOrDefault(line, "start", options),
		End:   getOptionOrDefault(line, "end", options),
		Style: getStyleOrDefault(line, options),
	}
}

func mergeOptions(primary, secondary FormatLineOptions) FormatLineOptions {
	ifEmpty := func(a, b string) string {
		if a == "" {
			return b
		}
		return a
	}

	ifNil := func(a, b func(line string) string) func(line string) string {
		if a == nil {
			return b
		}
		return a
	}

	return FormatLineOptions{
		Start: ifEmpty(primary.Start, secondary.Start),
		End:   ifEmpty(primary.End, secondary.End),
		Style: ifNil(primary.Style, secondary.Style),
	}
}

// FormatLines applies styles to multiple lines based on their type and the provided options.
func (p *Prompt[TValue]) FormatLines(lines []string, options FormatLinesOptions) string {
	terminalWidth, _, err := p.Size()
	if err != nil || terminalWidth <= 0 {
		terminalWidth = 80
	}
	if options.MaxWidth == 0 {
		options.MaxWidth = math.MaxInt
	}
	minWidth := max(options.MinWidth, 0)
	maxWith := min(options.MaxWidth, terminalWidth)
	if maxWith < 1 {
		maxWith = 1
	}

	firstLine := getLineOptions(options, FirstLine)
	newLine := getLineOptions(options, NewLine)
	lastLine := getLineOptions(options, LastLine)

	formattedLines := make([]string, 0, len(lines))
	for i, line := range lines {
		var opts FormatLineOptions
		switch {
		case i == 0 && len(lines) == 1:
			opts = mergeOptions(lastLine, firstLine)
		case i == 0:
			opts = firstLine
		case i == 1 && len(lines) == 2:
			opts = mergeOptions(lastLine, newLine)
		case i+1 == len(lines):
			opts = lastLine
		default:
			opts = newLine
		}

		// If empty slot > 0. Sum empty space
		startEmptySlots := utils.StrLength(opts.Start)
		if startEmptySlots > 0 {
			startEmptySlots++
		}
		endEmptySlots := utils.StrLength(opts.End)
		if endEmptySlots > 0 {
			endEmptySlots++
		}
		emptySlots := startEmptySlots + endEmptySlots + utils.StrLength(opts.Style(""))

		formatAndAddLine := func(line string) {
			var startSpace, endSpace string
			if startEmptySlots > 0 {
				startSpace = " "
			}
			styledLine := opts.Style(line)
			if minWidth > 0 {
				endSpace = strings.Repeat(" ", max(minWidth+1-startEmptySlots-utils.StrLength(styledLine)-endEmptySlots, 0))
			} else if endEmptySlots > 0 {
				endSpace = " "
			}
			formattedLine := strings.Join([]string{opts.Start, startSpace, styledLine, endSpace, opts.End}, "")
			formattedLines = append(formattedLines, formattedLine)
		}

		if len(lines) == 1 && strings.Trim(lines[0], " ") == "" {
			formatAndAddLine("")
			break
		}

		var currentLine string
		for _, word := range strings.Split(line, " ") {
			switch {
			case word == "":
				currentLine += " "
			case strings.Trim(currentLine, " ") == "" && utils.StrLength(currentLine+word)+emptySlots <= maxWith:
				currentLine += word
			case utils.StrLength(currentLine+word)+emptySlots+1 <= maxWith:
				currentLine += " " + word
			case utils.StrLength(word)+emptySlots >= maxWith:
				var splitIndex int
				if utils.StrLength(currentLine) == 0 {
					splitIndex = maxWith - emptySlots
					splitIndex = min(max(splitIndex, 1), len(word))
					formatAndAddLine(word[0:splitIndex])
				} else {
					splitIndex = maxWith - utils.StrLength(currentLine) - emptySlots - 1
					splitIndex = min(max(splitIndex, 1), len(word))
					formatAndAddLine(currentLine + " " + word[0:splitIndex])
				}

				chunkLength := max(maxWith-emptySlots, 1)
				chunk := word[splitIndex:]
				for utils.StrLength(chunk) > chunkLength {
					formatAndAddLine(chunk[0:chunkLength])
					chunk = chunk[chunkLength:]
				}

				currentLine = chunk
			default:
				formatAndAddLine(currentLine)
				currentLine = word
			}
		}
		formatAndAddLine(currentLine)
	}

	return strings.Join(formattedLines, "\r\n")
}
