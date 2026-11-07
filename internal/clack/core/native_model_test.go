package core

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNativeCancelIgnoresStaleValidation(t *testing.T) {
	p := NewPrompt(PromptParams[string]{Validate: func(_ string) error { return errors.New("should be ignored") }, Render: func(_ *Prompt[string]) string { return "test" }})
	p.Init()
	_, cmd := p.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil || !p.IsValidating {
		t.Fatal("async validator did not start")
	}
	token := p.validationID
	p.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if p.State != CancelState || p.IsValidating {
		t.Fatalf("cancel did not take effect: %v", p.State)
	}
	p.Update(promptValidatedMsg{owner: p, id: token})
	if p.State != CancelState {
		t.Fatal("stale validation submitted a canceled prompt")
	}
}

func TestNativePasteUTF8AndHeader(t *testing.T) {
	p := NewPrompt(PromptParams[string]{Render: func(p *Prompt[string]) string { return "value:" + p.Value }})
	p.HandleKey = func(key *Key) { p.Value, p.CursorIndex = p.TrackKeyValue(key, p.Value, p.CursorIndex) }
	p.Init()
	p.Update(tea.PasteMsg{Content: "日本語\n😀"})
	if p.Value != "日本語😀" {
		t.Fatalf("paste value=%q", p.Value)
	}
	if p.View().Content != "value:日本語😀" {
		t.Fatalf("view=%q", p.View().Content)
	}
}
