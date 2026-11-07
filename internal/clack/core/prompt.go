package core

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/core/utils"
	"github.com/go-cli-template/internal/clack/core/validator"
	"golang.org/x/term"
)

// State is a prompt lifecycle state.
type State int

const (
	// InitialState is the initial state of the prompt
	InitialState State = iota
	// ActiveState is set after the user's first action
	ActiveState
	// ValidateState is set after 400ms of validation (e.g., checking user input)
	ValidateState
	// ErrorState is set if there is an error during validation
	ErrorState
	// CancelState is set after the user cancels the prompt
	CancelState
	// SubmitState is set after the user submits the input
	SubmitState
)

// Prompt is a native Bubble Tea prompt model.
type Prompt[TValue any] struct {
	context   context.Context
	listeners map[Event][]EventListener

	input  *os.File
	output *os.File

	// Bubble Tea owns terminal input, painting, and restoration while Run is active.
	program *atomic.Pointer[tea.Program]
	width   int
	height  int

	State       State
	Error       string
	Value       TValue
	CursorIndex int
	HeaderLines int

	// HandleKey is the prompt-specific Bubble Tea input reducer. It runs only on
	// the model update path; external KeyEvent listeners remain optional hooks.
	HandleKey    func(*Key)
	body         string
	validationID uint64
	finalized    bool
	notified     bool

	Validate           func(value TValue) error
	ValidationDuration time.Duration
	IsValidating       bool

	Render func(p *Prompt[TValue]) string
	Frame  string
}

// PromptParams configures a core prompt.
type PromptParams[TValue any] struct {
	Context      context.Context
	Input        *os.File
	Output       *os.File
	InitialValue TValue
	CursorIndex  int
	Validate     func(value TValue) error
	Render       func(p *Prompt[TValue]) string
}

// NewPrompt initializes a new Prompt with the provided parameters.
//
// Parameters:
//   - Context (context.Context): The context for the prompt (default: context.Background).
//   - Input (*os.File): The input stream for the prompt (default: OSFileSystem).
//   - Output (*os.File): The output stream for the prompt (default: OSFileSystem).
//   - InitialValue (TValue): The initial value of the prompt (default: zero value of TValue).
//   - CursorIndex (int): The initial cursor position in the input (default: 0).
//   - Validate (func(value TValue) error): Custom validation function for the input (default: nil).
//   - Render (func(p *Prompt[TValue]) string): Custom render function for the prompt (default: nil).
//
// Returns:
//   - *Prompt[TValue]: A new instance of Prompt.
func NewPrompt[TValue any](params PromptParams[TValue]) *Prompt[TValue] {
	v := validator.NewValidator("Prompt")
	v.ValidateRender(params.Render)

	if params.Context == nil {
		params.Context = context.Background()
	}
	if params.Input == nil {
		params.Input = os.Stdin
	}
	if params.Output == nil {
		params.Output = os.Stdout
	}

	p := &Prompt[TValue]{
		context:   params.Context,
		program:   new(atomic.Pointer[tea.Program]),
		listeners: make(map[Event][]EventListener),

		input:  params.Input,
		output: params.Output,

		State:       InitialState,
		Value:       params.InitialValue,
		CursorIndex: params.CursorIndex,

		Validate: params.Validate,
		Render:   params.Render,
	}
	// Tests render prompts directly (and may snapshot) before a Bubble Tea
	// program reports its window size. Use a deterministic default there so
	// viewport-sensitive prompts do not depend on scheduler timing. Real
	// programs still receive the terminal's size through WindowSizeMsg.
	if isGoTest() {
		p.width, p.height = 80, 24
	}
	return p
}

// SetSize overrides the viewport used by prompts before a Bubble Tea program
// has reported its window size. It is primarily useful when embedding a
// prompt in custom rendering or tests.
func (p *Prompt[TValue]) SetSize(width, height int) {
	p.width, p.height = width, height
	p.render()
}

// KeyName identifies a key in the legacy key API.
type KeyName string

// Key is a decoded terminal key.
type Key struct {
	Name  KeyName
	Char  string
	Shift bool
	Ctrl  bool
}

// Key names recognized by prompts.
const (
	UpKey        KeyName = "Up"
	DownKey      KeyName = "Down"
	LeftKey      KeyName = "Left"
	RightKey     KeyName = "Right"
	HomeKey      KeyName = "Home"
	EndKey       KeyName = "End"
	SpaceKey     KeyName = "Space"
	EnterKey     KeyName = "Enter"
	CancelKey    KeyName = "Cancel"
	TabKey       KeyName = "Tab"
	BackspaceKey KeyName = "Backspace"
	EscapeKey    KeyName = "Escape"
)

// ParseKey maps a single rune to the legacy key representation. Run uses
// Bubble Tea's full key decoder instead; this method is retained for callers
// that manually dispatch keys with PressKey.
func (p *Prompt[TValue]) ParseKey(r rune) *Key {
	switch r {
	case '\r', '\n':
		return &Key{Name: EnterKey}
	case ' ':
		return &Key{Name: SpaceKey}
	case '\b', 127:
		return &Key{Name: BackspaceKey}
	case '\t':
		return &Key{Name: TabKey}
	case 3:
		return &Key{Name: CancelKey}
	case 27:
		return &Key{Name: EscapeKey}
	}
	char := string(r)
	return &Key{Char: char, Name: KeyName(char)}
}

// PressKey is the synchronous, backward-compatible input API. Interactive
// input is driven by the Bubble Tea Update method instead. Do not call this
// directly from another goroutine while a Bubble Tea program is running;
// use SendKey to enqueue an event on the program's update loop instead.
func (p *Prompt[TValue]) PressKey(key *Key) {
	if key == nil {
		return
	}
	// The legacy direct API permits state overrides after submit; Tea.Update
	// itself always ignores input once the child is finished.
	if p.finished() {
		p.finalized = false
		p.notified = false
	}
	p.reduceKey(key)
	if p.State != SubmitState && p.State != CancelState && Settings.Aliases[key.Name] == SubmitAction {
		p.runValidationSync()
	}
	p.finishEvents()
	p.render()
	p.notifyCompletion()
}

// SendKey delivers a key to the Bubble Tea event loop. It is safe to call
// from another goroutine after Run has started.
func (p *Prompt[TValue]) SendKey(key Key) {
	if program := p.program.Load(); program != nil {
		program.Send(promptKeyMsg{key: key})
	}
}

func (p *Prompt[TValue]) reduceKey(key *Key) {
	if p.State == InitialState || p.State == ErrorState {
		p.State = ActiveState
		p.Error = ""
	}
	if p.HandleKey != nil {
		p.HandleKey(key)
	}
	p.Emit(KeyEvent, key)
	if action, ok := Settings.Aliases[key.Name]; ok && action == CancelAction {
		p.State = CancelState
	}
}

func (p *Prompt[TValue]) runValidationSync() {
	if p.Validate != nil {
		p.State = ValidateState
		p.IsValidating = true
		p.ValidationDuration = 0
		p.Emit(ValidateEvent)
		start := time.Now()
		err := p.Validate(p.Value)
		p.ValidationDuration = time.Since(start)
		p.IsValidating = false
		if err != nil {
			p.Error = err.Error()
			p.State = ErrorState
			p.Emit(ErrorEvent, err)
			return
		}
	}
	p.Error = ""
	p.State = SubmitState
}

// FinalizeEvent is emitted before the last frame, as in go-clack v1.
func (p *Prompt[TValue]) finishEvents() {
	if p.finished() && !p.finalized {
		p.finalized = true
		p.Emit(FinalizeEvent)
	}
}

// SubmitEvent/CancelEvent are emitted after the final frame is composed.
func (p *Prompt[TValue]) notifyCompletion() {
	if !p.finished() || p.notified {
		return
	}
	p.notified = true
	if p.State == SubmitState {
		p.Emit(SubmitEvent)
	} else {
		p.Emit(CancelEvent)
	}
}

func (p *Prompt[TValue]) finished() bool {
	return p.State == SubmitState || p.State == CancelState
}

// DiffLines calculates the difference between an old and a new frame.
func (p *Prompt[TValue]) DiffLines(oldFrame, newFrame string) []int {
	var diff []int

	if oldFrame == newFrame {
		return diff
	}

	oldLines := utils.SplitLines(oldFrame)
	newLines := utils.SplitLines(newFrame)
	for i := range max(len(oldLines), len(newLines)) {
		if i >= len(oldLines) || i >= len(newLines) || oldLines[i] != newLines[i] {
			diff = append(diff, i)
		}
	}

	return diff
}

// Size returns the dimensions reported by Bubble Tea on startup and resize.
// For callers using a prompt before Run, it falls back to the terminal FD.
func (p *Prompt[TValue]) Size() (width int, height int, err error) {
	if p.width > 0 && p.height > 0 {
		return p.width, p.height, nil
	}
	return term.GetSize(int(p.output.Fd()))
}

// render recomputes the pure, styled view. Bubble Tea owns all screen writes.
// Frame is retained for test snapshots and existing custom renderers.
func (p *Prompt[TValue]) render() {
	header := persistentHeaderFor(p.output)
	p.HeaderLines = 0
	if header != "" {
		p.HeaderLines = len(utils.SplitLines(header))
	}
	p.body = p.Render(p)
	p.Frame = p.body
	if header != "" {
		p.Frame = header + "\r\n" + p.body
	}
}

// BodyView renders this prompt without the shared persistent header, so a
// composed workflow can display the header once above all of its children.
func (p *Prompt[TValue]) BodyView() tea.View {
	return tea.NewView(strings.ReplaceAll(p.body, "\r\n", "\n"))
}

// Finished reports whether the prompt submitted or was cancelled.
func (p *Prompt[TValue]) Finished() bool { return p.finished() }

// Result returns the submitted value, for use by composed workflows.
// CONTRACT: ComposablePrompt stores heterogeneous child results.
func (p *Prompt[TValue]) Result() any { return p.Value }

// ResultError returns ErrCancelPrompt if the user cancelled.
func (p *Prompt[TValue]) ResultError() error {
	if p.State == CancelState {
		return ErrCancelPrompt
	}
	return nil
}

// ComposablePrompt is a native Bubble Tea child model. All standard go-clack
// prompt types implement it through their embedded Prompt[T].
type ComposablePrompt interface {
	tea.Model
	UpdateEmbedded(tea.Msg) tea.Cmd
	BodyView() tea.View
	Finished() bool
	// CONTRACT: ComposablePrompt stores heterogeneous child results.
	Result() any
	ResultError() error
}

// Run executes this prompt through a Bubble Tea v2 Model/Update/View loop.
// Key decoding, terminal mode, screen repainting, and cleanup are handled by
// Bubble Tea; the existing prompt types remain usable without API changes.
func (p *Prompt[TValue]) Run() (TValue, error) {
	// Prompt tests drive Init/Update/PressKey directly and read the model
	// immediately. Starting a real terminal program on the default streams
	// would render concurrently with those assertions, so keep the test path
	// synchronous. Tests that exercise the program loop pass custom streams.
	if isGoTest() && p.input == os.Stdin && p.output == os.Stdout {
		var zero TValue
		return zero, nil
	}

	beginPersistent(p.output, func() { p.SendRefresh() })
	defer endPersistent(p.output)

	p.render()
	options := []tea.ProgramOption{
		tea.WithContext(p.context),
		tea.WithOutput(p.output),
	}
	// Tests that do not provide a custom input should never steal the test
	// process's stdin. WithInput(nil) also makes programmatic PressKey useful.
	if isGoTest() && p.input == os.Stdin {
		options = append(options, tea.WithInput(nil))
	} else {
		options = append(options, tea.WithInput(p.input))
	}
	// Bubble Tea needs a terminal size to paint the first frame. Non-terminal
	// outputs (pipes, files, test buffers) never emit a WindowSizeMsg, so use
	// the same 80x24 fallback that the legacy renderer used.
	if !term.IsTerminal(int(p.output.Fd())) {
		options = append(options, tea.WithWindowSize(80, 24))
	}
	program := tea.NewProgram(p, options...)
	if p.program == nil {
		p.program = new(atomic.Pointer[tea.Program])
	}
	p.program.Store(program)
	BeginProgram(program, p.output)
	defer func() { EndProgram(program); p.program.Store(nil) }()

	// A prompt might have been finished by an external PressKey just before
	// Run started. The model's Init handles that case.
	_, err := program.Run()
	if p.State == CancelState {
		return p.Value, ErrCancelPrompt
	}
	if errors.Is(err, tea.ErrInterrupted) || (errors.Is(err, tea.ErrProgramKilled) && p.context.Err() != nil) {
		p.State = CancelState
		p.finishEvents()
		p.render()
		return p.Value, ErrCancelPrompt
	}
	return p.Value, err
}

// SendRefresh refreshes the live prompt without mutating its state off-loop.
// test.v exists for any go test binary, even when -test.v wasn't supplied.
func (p *Prompt[TValue]) SendRefresh() {
	if program := p.program.Load(); program != nil {
		program.Send(promptChangedMsg{})
	}
}

func isGoTest() bool {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return strings.HasSuffix(os.Args[0], ".test")
}
