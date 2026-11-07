// Package core implements the native Bubble Tea prompt runtime.
package core

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// Prompt[T] itself is a Bubble Tea v2 model. Individual prompt constructors
// supply HandleKey reducers and the original go-clack style renderer.
type promptChangedMsg struct{}
type promptKeyMsg struct{ key Key }
type promptValidatedMsg struct {
	owner any
	id    uint64
	err   error
}
type validationTickMsg struct {
	owner any
	id    uint64
	at    time.Time
}

// Init initializes the prompt model.
func (p *Prompt[T]) Init() tea.Cmd {
	p.render()
	if p.finished() {
		return tea.Quit
	}
	return nil
}

// Update handles Bubble Tea messages.
func (p *Prompt[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return p, p.update(msg, true)
}

// UpdateEmbedded lets a parent Bubble Tea model drive this prompt without
// prematurely quitting the parent program when the child finishes.
func (p *Prompt[T]) UpdateEmbedded(msg tea.Msg) tea.Cmd {
	return p.update(msg, false)
}

func (p *Prompt[T]) update(msg tea.Msg, ownProgram bool) tea.Cmd {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key := keyFromBubbleTea(msg); key != nil {
			cmd = p.acceptKey(key)
		}
	case promptKeyMsg:
		cmd = p.acceptKey(&msg.key)
	case tea.PasteMsg:
		if p.finished() || p.IsValidating {
			return nil
		}
		for _, r := range msg.Content {
			if unicode.IsControl(r) {
				continue
			}
			key := &Key{Name: KeyName(string(r)), Char: string(r)}
			if r == ' ' {
				key.Name = SpaceKey
			}
			p.reduceKey(key)
		}
		p.render()
	case promptValidatedMsg:
		if !p.IsValidating || (msg.id != p.validationID || msg.owner != p) {
			return nil
		}
		p.IsValidating = false
		if msg.err != nil {
			p.State = ErrorState
			p.Error = msg.err.Error()
			p.Emit(ErrorEvent, msg.err)
		} else {
			p.State = SubmitState
			p.Error = ""
			p.finishEvents()
		}
		p.render()
		p.notifyCompletion()
	case validationTickMsg:
		if !p.IsValidating || (msg.id != p.validationID || msg.owner != p) {
			return nil
		}
		p.ValidationDuration = time.Since(msg.at)
		p.render()
		return tea.Tick(125*time.Millisecond, func(time.Time) tea.Msg { return validationTickMsg{owner: p, id: msg.id, at: msg.at} })
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.render()
	case promptChangedMsg:
		p.render()
	}
	if ownProgram && p.finished() {
		return tea.Quit
	}
	return cmd
}

func (p *Prompt[T]) acceptKey(key *Key) tea.Cmd {
	if p.finished() {
		return nil
	}
	if p.IsValidating {
		if action, ok := Settings.Aliases[key.Name]; ok && action == CancelAction {
			p.IsValidating = false
			p.validationID++ // invalidate any outstanding command
			p.State = CancelState
			p.finishEvents()
			p.render()
			p.notifyCompletion()
		}
		return nil
	}
	p.reduceKey(key)
	if p.finished() {
		p.finishEvents()
		p.render()
		p.notifyCompletion()
		return nil
	}
	if Settings.Aliases[key.Name] == SubmitAction {
		if p.Validate == nil {
			p.State = SubmitState
			p.finishEvents()
		} else {
			p.State = ValidateState
			p.IsValidating = true
			p.ValidationDuration = 0
			p.Emit(ValidateEvent)
			p.validationID++
			id := p.validationID
			start := time.Now()
			value, validate := p.Value, p.Validate
			p.render()
			return tea.Batch(
				func() tea.Msg { return promptValidatedMsg{owner: p, id: id, err: validate(value)} },
				tea.Tick(125*time.Millisecond, func(time.Time) tea.Msg { return validationTickMsg{owner: p, id: id, at: start} }),
			)
		}
	}
	p.render()
	p.notifyCompletion()
	return nil
}

// View renders the prompt frame.
func (p *Prompt[T]) View() tea.View {
	return tea.NewView(strings.ReplaceAll(p.Frame, "\r\n", "\n"))
}

func keyFromBubbleTea(msg tea.KeyPressMsg) *Key {
	switch msg.String() {
	case "up":
		return &Key{Name: UpKey}
	case "down":
		return &Key{Name: DownKey}
	case "left":
		return &Key{Name: LeftKey}
	case "right":
		return &Key{Name: RightKey}
	case "home", "ctrl+a":
		return &Key{Name: HomeKey}
	case "end", "ctrl+e":
		return &Key{Name: EndKey}
	case "space":
		return &Key{Name: SpaceKey}
	case "enter":
		return &Key{Name: EnterKey}
	case "ctrl+c":
		return &Key{Name: CancelKey, Ctrl: true}
	case "esc":
		return &Key{Name: EscapeKey}
	case "backspace":
		return &Key{Name: BackspaceKey}
	case "tab":
		return &Key{Name: TabKey}
	case "shift+tab":
		return &Key{Name: TabKey, Shift: true}
	}
	k := msg.Key()
	if k.Text != "" {
		return &Key{Name: KeyName(k.Text), Char: k.Text}
	}
	name := msg.String()
	if name != "" {
		return &Key{Name: KeyName(name), Ctrl: strings.HasPrefix(name, "ctrl+"), Shift: strings.Contains(name, "shift+")}
	}
	return nil
}
