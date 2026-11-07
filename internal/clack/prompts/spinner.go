package prompts

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
	isunicodesupported "github.com/go-cli-template/internal/clack/third_party/is-unicode-supported"
	"github.com/go-cli-template/internal/clack/third_party/picocolors"
	"golang.org/x/term"
)

// SpinnerIndicator defines the type for spinner indicators.
type SpinnerIndicator int

// Spinner indicator modes.
const (
	SpinnerDotsIndicator SpinnerIndicator = iota
	SpinnerStaticDotsIndicator
	SpinnerTimerIndicator
)

// SpinnerOptions configures a spinner.
type SpinnerOptions struct {
	Context       context.Context
	Output        io.Writer
	Indicator     SpinnerIndicator
	Frames        []string
	FrameInterval time.Duration
	OnCancel      func()
	CancelMessage string
	ErrorMessage  string
}

// SpinnerController keeps the original Start/Message/Stop API. A dedicated
// Bubble Tea program owns all rendering and terminal cleanup.
type SpinnerController struct {
	options     SpinnerOptions
	isCI        bool
	IsCancelled bool

	mu      sync.Mutex
	program *tea.Program
	done    chan struct{}
	stopped bool
}

// Spinner starts a spinner and returns its controller.
func Spinner(options SpinnerOptions) *SpinnerController {
	isCI := os.Getenv("CI") == "true"
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Output == nil {
		options.Output = os.Stdout
	}
	unicode := isunicodesupported.IsUnicodeSupported()
	if len(options.Frames) == 0 {
		if unicode {
			options.Frames = []string{"◒", "◐", "◓", "◑"}
		} else {
			options.Frames = []string{"•", "o", "O", "0"}
		}
	}
	if options.FrameInterval <= 0 {
		if unicode {
			options.FrameInterval = 80 * time.Millisecond
		} else {
			options.FrameInterval = 120 * time.Millisecond
		}
	}
	if isCI {
		options.Indicator = SpinnerStaticDotsIndicator
	}
	return &SpinnerController{options: options, isCI: isCI}
}

type spinnerTickMsg time.Time
type spinnerMessageMsg string
type spinnerStopMsg struct {
	text string
	code int
}

type spinnerModel struct {
	controller *SpinnerController
	message    string
	frameIndex int
	started    time.Time
	finished   bool
	elapsed    time.Duration
	final      string
}

func (m *spinnerModel) Init() tea.Cmd {
	return tea.Tick(m.controller.options.FrameInterval, func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

func (m *spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerTickMsg:
		if m.finished {
			return m, nil
		}
		m.elapsed = time.Time(msg).Sub(m.started)
		if !m.controller.isCI {
			m.frameIndex = (m.frameIndex + 1) % len(m.controller.options.Frames)
		}
		return m, tea.Tick(m.controller.options.FrameInterval, func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
	case spinnerMessageMsg:
		m.message = trimSpinnerDots(string(msg))
	case spinnerStopMsg:
		m.finish(msg.text, msg.code)
		return m, tea.Quit
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.controller.mu.Lock()
			m.controller.IsCancelled = true
			m.controller.mu.Unlock()
			text := m.controller.options.CancelMessage
			if text == "" {
				text = core.Settings.Messages.CancelMessage
			}
			m.finish(text, 1)
			if m.controller.options.OnCancel != nil {
				go m.controller.options.OnCancel()
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *spinnerModel) finish(text string, code int) {
	if m.finished {
		return
	}
	m.finished = true
	step := picocolors.Green(symbols.StepSubmit)
	if code == 1 {
		step = picocolors.Red(symbols.StepCancel)
	}
	if code != 0 && code != 1 {
		step = picocolors.Red(symbols.StepError)
	}
	if text != "" {
		m.message = regexp.MustCompile(`\.{2,}$`).ReplaceAllString(text, ".")
	}
	m.final = fmt.Sprintf("%s %s\n", step, m.message)
}

func (m *spinnerModel) View() tea.View {
	if m.finished {
		return tea.NewView(m.final)
	}
	s := m.controller
	frame := picocolors.Magenta(s.options.Frames[m.frameIndex])
	body := s.formatFrame(s.options.Indicator, frame, m.message, m.elapsed)
	return tea.NewView(picocolors.Gray(symbols.Bar) + "\n" + body)
}

// Start launches a Bubble Tea model without blocking the caller. Bubble Tea
// handles tick scheduling, input decoding and repainting (no manual ANSI erase).
func (s *SpinnerController) Start(msg string) {
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return
	}
	s.done = make(chan struct{})
	model := &spinnerModel{controller: s, message: trimSpinnerDots(msg), started: time.Now()}
	options := []tea.ProgramOption{tea.WithContext(s.options.Context), tea.WithOutput(s.options.Output)}
	// A non-TTY output (such as a task log or a test writer) should not
	// take ownership of stdin. Actual terminal use still supports Ctrl+C.
	if s.options.Output == os.Stdout && term.IsTerminal(int(os.Stdin.Fd())) {
		options = append(options, tea.WithInput(os.Stdin))
	} else {
		options = append(options, tea.WithInput(nil), tea.WithWindowSize(80, 24))
	}
	s.program = tea.NewProgram(model, options...)
	program := s.program
	core.BeginProgram(program, s.options.Output)
	done := s.done
	s.mu.Unlock()
	go func() {
		defer core.EndProgram(program)
		_, err := program.Run()
		if err != nil && s.options.Context.Err() != nil {
			s.mu.Lock()
			s.IsCancelled = true
			s.mu.Unlock()
			if s.options.OnCancel != nil {
				s.options.OnCancel()
			}
		}
		close(done)
	}()
}

func trimSpinnerDots(msg string) string {
	return regexp.MustCompile(`\.+$`).ReplaceAllString(msg, "")
}

// Message updates the spinner message.
func (s *SpinnerController) Message(msg string) {
	s.mu.Lock()
	program, stopped := s.program, s.stopped
	s.mu.Unlock()
	if program != nil && !stopped {
		program.Send(spinnerMessageMsg(msg))
	}
}

// Stop posts the final frame through Bubble Tea and waits for terminal cleanup.
func (s *SpinnerController) Stop(msg string, code int) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	program, done := s.program, s.done
	s.mu.Unlock()
	if program == nil {
		return
	}
	program.Send(spinnerStopMsg{text: msg, code: code})
	<-done
}

func (s *SpinnerController) formatFrame(indicator SpinnerIndicator, frame string, message string, duration time.Duration) string {
	switch indicator {
	case SpinnerDotsIndicator:
		dotsCounter := int(duration.Seconds()) % 4
		return fmt.Sprintf("%s %s%s", frame, message, strings.Repeat(".", dotsCounter))
	case SpinnerStaticDotsIndicator:
		return fmt.Sprintf("%s %s...", frame, message)
	case SpinnerTimerIndicator:
		minutes := int(duration.Minutes())
		secs := int(duration.Seconds()) - (minutes * 60)
		if minutes > 0 {
			return fmt.Sprintf("%s %s [%dm %ds]", frame, message, minutes, secs)
		}
		return fmt.Sprintf("%s %s [%ds]", frame, message, secs)
	default:
		return fmt.Sprintf("%s %s", frame, message)
	}
}
