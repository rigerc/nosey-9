package prompts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
	"golang.org/x/term"
)

// Task represents a unit of work displayed as a go-clack spinner.
// ContextTask is preferred for cancellable work; Task is preserved for
// backwards compatibility with code using the original callback shape.
type Task struct {
	Title       string
	Task        func(message func(string)) (string, error)
	ContextTask func(context.Context, func(string)) (string, error)
	Disabled    bool
}

// Tasks retains the original API while running all tasks in ONE Bubble Tea
// program, rather than launching a nested program for every spinner.
func Tasks(tasks []Task, options SpinnerOptions) { _ = RunTasks(tasks, options) }

// RunTasks runs a sequence of background commands and reports task failures.
// A failure renders the original red step and does not prevent later tasks.
func RunTasks(tasks []Task, options SpinnerOptions) error {
	s := Spinner(options)
	m := &tasksModel{tasks: tasks, spinner: s, index: -1}
	opts := []tea.ProgramOption{tea.WithContext(s.options.Context), tea.WithOutput(s.options.Output)}
	if s.options.Output == os.Stdout && term.IsTerminal(int(os.Stdin.Fd())) {
		opts = append(opts, tea.WithInput(os.Stdin))
	} else {
		opts = append(opts, tea.WithInput(nil), tea.WithWindowSize(80, 24))
	}
	program := tea.NewProgram(m, opts...)
	m.program = program
	core.BeginProgram(program, s.options.Output)
	defer core.EndProgram(program)
	_, err := program.Run()
	if m.cancel != nil {
		return m.cancel
	}
	return errors.Join(append(m.errs, err)...)
}

type taskProgressMsg struct {
	index int
	text  string
}
type taskCompleteMsg struct {
	index int
	text  string
	err   error
}
type taskPulseMsg time.Time

type tasksModel struct {
	tasks      []Task
	spinner    *SpinnerController
	program    *tea.Program
	cancelFunc context.CancelFunc
	index      int
	message    string
	frame      int
	started    time.Time
	history    []string
	errs       []error
	cancel     error
	finished   bool
}

func (m *tasksModel) Init() tea.Cmd {
	return tea.Batch(m.nextTask(), tea.Tick(m.spinner.options.FrameInterval, func(t time.Time) tea.Msg { return taskPulseMsg(t) }))
}

func (m *tasksModel) nextTask() tea.Cmd {
	if m.cancelFunc != nil {
		m.cancelFunc()
		m.cancelFunc = nil
	}
	for {
		m.index++
		if m.index >= len(m.tasks) {
			m.finished = true
			return tea.Quit
		}
		if !m.tasks[m.index].Disabled {
			break
		}
	}
	item := m.tasks[m.index]
	m.message = trimSpinnerDots(item.Title)
	m.started = time.Now()
	m.frame = 0
	ctx, cancel := context.WithCancel(m.spinner.options.Context)
	m.cancelFunc = cancel
	idx := m.index
	publish := func(s string) {
		if m.program != nil {
			m.program.Send(taskProgressMsg{index: idx, text: s})
		}
	}
	return func() tea.Msg {
		if item.ContextTask != nil {
			value, err := item.ContextTask(ctx, publish)
			return taskCompleteMsg{index: idx, text: value, err: err}
		}
		if item.Task == nil {
			return taskCompleteMsg{index: idx, err: errors.New("task has no callback")}
		}
		value, err := item.Task(publish)
		return taskCompleteMsg{index: idx, text: value, err: err}
	}
}

func (m *tasksModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.finished {
		return m, nil
	}
	switch msg := msg.(type) {
	case taskProgressMsg:
		if msg.index == m.index {
			m.message = trimSpinnerDots(msg.text)
		}
	case taskCompleteMsg:
		if msg.index != m.index {
			break
		}
		display := msg.text
		symbol := picocolors.Green(symbols.StepSubmit)
		if msg.err != nil {
			m.errs = append(m.errs, fmt.Errorf("%s: %w", m.tasks[m.index].Title, msg.err))
			symbol = picocolors.Red(symbols.StepError)
			display = msg.err.Error()
		}
		if display == "" {
			display = m.message
		}
		m.history = append(m.history, fmt.Sprintf("%s %s", symbol, display))
		return m, m.nextTask()
	case taskPulseMsg:
		if !m.spinner.isCI {
			m.frame = (m.frame + 1) % len(m.spinner.options.Frames)
		}
		return m, tea.Tick(m.spinner.options.FrameInterval, func(t time.Time) tea.Msg { return taskPulseMsg(t) })
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.finished = true
			m.cancel = core.ErrCancelPrompt
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			m.spinner.mu.Lock()
			m.spinner.IsCancelled = true
			m.spinner.mu.Unlock()
			text := m.spinner.options.CancelMessage
			if text == "" {
				text = core.Settings.Messages.CancelMessage
			}
			m.history = append(m.history, picocolors.Red(symbols.StepCancel)+" "+text)
			if m.spinner.options.OnCancel != nil {
				m.spinner.options.OnCancel()
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *tasksModel) View() tea.View {
	parts := append([]string(nil), m.history...)
	if !m.finished && m.index >= 0 && m.index < len(m.tasks) {
		frame := picocolors.Magenta(m.spinner.options.Frames[m.frame])
		parts = append(parts, picocolors.Gray(symbols.Bar), m.spinner.formatFrame(m.spinner.options.Indicator, frame, m.message, time.Since(m.started)))
	}
	return tea.NewView(strings.Join(parts, "\n"))
}
