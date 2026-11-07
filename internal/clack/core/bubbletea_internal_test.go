package core

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBubbleTeaModelReceivesUTF8Input(t *testing.T) {
	p := NewPrompt(PromptParams[string]{Render: func(p *Prompt[string]) string { return p.Value }})
	// CONTRACT: core.Prompt.On sets the variadic EventListener signature.
	p.On(KeyEvent, func(args ...any) {
		// SAFETY: KeyEvent listeners receive exactly one *Key payload.
		p.Value, p.CursorIndex = p.TrackKeyValue(args[0].(*Key), p.Value, p.CursorIndex)
	})
	p.render()
	model := p
	model.Update(tea.KeyPressMsg(tea.Key{Code: 'é', Text: "é"}))
	model.Update(tea.KeyPressMsg(tea.Key{Code: '世', Text: "世"}))
	if p.Value != "é世" || p.CursorIndex != len("é世") {
		t.Fatalf("UTF-8 insert: value=%q cursor=%d", p.Value, p.CursorIndex)
	}
	if model.View().Content != "é世" {
		t.Fatalf("unexpected frame %q", model.View().Content)
	}
	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if p.Value != "世" || p.CursorIndex != 0 {
		t.Fatalf("UTF-8 backspace: value=%q cursor=%d", p.Value, p.CursorIndex)
	}
}

func TestBubbleTeaModelValidatesAsCommand(t *testing.T) {
	p := NewPrompt(PromptParams[string]{
		InitialValue: "value",
		Validate: func(v string) error {
			if v != "value" {
				return errors.New("invalid")
			}
			return nil
		},
		Render: func(p *Prompt[string]) string { return p.Value },
	})
	p.render()
	m := p
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil || p.State != ValidateState || !p.IsValidating {
		t.Fatalf("validation did not yield a command: state=%v", p.State)
	}
	m.Update(promptValidatedMsg{owner: p, id: p.validationID, err: errors.New("failed")})
	if p.State != ErrorState || p.IsValidating || p.Error != "failed" {
		t.Fatalf("validation error not propagated: state=%v error=%q", p.State, p.Error)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.Update(promptValidatedMsg{owner: p, id: p.validationID})
	if p.State != SubmitState || p.Error != "" {
		t.Fatalf("validation did not submit: state=%v error=%q", p.State, p.Error)
	}
}

func TestBubbleTeaModelResize(t *testing.T) {
	p := NewPrompt(PromptParams[string]{Render: func(_ *Prompt[string]) string { return "width" }})
	m := p
	m.Update(tea.WindowSizeMsg{Width: 42, Height: 12})
	width, height, err := p.Size()
	if err != nil || width != 42 || height != 12 {
		t.Fatalf("size = %d x %d (%v)", width, height, err)
	}
	if !strings.Contains(m.View().Content, "width") {
		t.Fatal("resize did not render")
	}
}
