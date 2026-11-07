package core

import (
	"io"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// The active Bubble Tea program owns the terminal. Standalone logs/notes
// still write to their requested Writer; messages during a live session are
// printed by Bubble Tea above the active view instead of corrupting it.
var console = struct {
	sync.RWMutex
	program *tea.Program
	output  io.Writer
}{}

// BeginProgram records the active Bubble Tea program.
func BeginProgram(program *tea.Program, output io.Writer) {
	console.Lock()
	console.program = program
	console.output = output
	console.Unlock()
}

// EndProgram clears the active Bubble Tea program.
func EndProgram(program *tea.Program) {
	console.Lock()
	if console.program == program {
		console.program = nil
		console.output = nil
	}
	console.Unlock()
}

// HasActiveProgram reports whether a Bubble Tea session already owns the
// terminal. Standalone animations must not start nested programs.
func HasActiveProgram() bool {
	console.RLock()
	defer console.RUnlock()
	return console.program != nil
}

// WriteStyled preserves go-clack ANSI styling and CRLF semantics on a normal
// writer, while using Bubble Tea for output during a live program.
func WriteStyled(out io.Writer, styled string) error {
	console.RLock()
	program := console.program
	active := program != nil && console.output == out
	console.RUnlock()
	if active {
		normalized := strings.ReplaceAll(styled, "\r\n", "\n")
		program.Println(strings.TrimSuffix(normalized, "\n"))
		return nil
	}
	_, err := io.WriteString(out, styled)
	return err
}
