// Package sisteransi provides ANSI escape sequences for cursor and screen control.
//
// Forked from https://github.com/terkelg/sisteransi/blob/master/src/index.js.
package sisteransi

import (
	"fmt"
)

var (
	// ESC is the ANSI escape sequence prefix.
	ESC = "\x1B"
	// CSI is the control sequence introducer.
	CSI = fmt.Sprintf("%s[", ESC)
)

// MoveCursorUp moves the cursor up n lines.
func MoveCursorUp(row int) string {
	return fmt.Sprintf("%s%dA", CSI, row)
}

// MoveCursorDown moves the cursor down n lines.
func MoveCursorDown(row int) string {
	return fmt.Sprintf("%s%dB", CSI, row)
}

// MoveCursor moves the cursor by x columns and y lines.
func MoveCursor(row int, col int) string {
	ret := ""

	if col < 0 {
		ret += fmt.Sprintf("%s%dD", CSI, -col)
	} else if col > 0 {
		ret += fmt.Sprintf("%s%dC", CSI, col)
	}

	if row < 0 {
		ret += MoveCursorUp(-row)
	} else if row > 0 {
		ret += MoveCursorDown(row)
	}

	return ret
}

// HideCursor hides the terminal cursor.
func HideCursor() string {
	return fmt.Sprintf("%s?25l", CSI)
}

// ShowCursor shows the terminal cursor.
func ShowCursor() string {
	return fmt.Sprintf("%s?25h", CSI)
}

// Restore restores the saved cursor position.
func Restore() string {
	return fmt.Sprintf("%s8", CSI)
}

// EraseCurrentLine clears the current line.
func EraseCurrentLine() string {
	return "\x1b[K"
}

// EraseDown clears from the cursor to the end of the screen.
func EraseDown() string {
	return "\x1b[J"
}
