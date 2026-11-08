// Package symbols defines the glyphs used by go-clack prompts.
package symbols

import (
	"github.com/rigerc/nosey-9/internal/clack/core"
	isunicodesupported "github.com/rigerc/nosey-9/internal/clack/third_party/is-unicode-supported"
)

func s(c, fallback string) string {
	if isunicodesupported.IsUnicodeSupported() {
		return c
	}
	return fallback
}

// Symbol is a terminal glyph.
type Symbol = string

// Prompt glyphs.
var (
	StepActive = s("◆", "*")
	StepCancel = s("■", "x")
	StepError  = s("▲", "x")
	StepSubmit = s("◇", "o")

	BarStart = s("┌", "T")
	Bar      = s("│", "|")
	BarEnd   = s("└", "—")

	RadioActive      = s("●", ">")
	RadioInactive    = s("○", " ")
	CheckboxActive   = s("◻", "[•]")
	CheckboxSelected = s("◼", "[+]")
	CheckboxInactive = s("◻", "[ ]")
	PasswordMask     = s("▪", "•")

	BarH              = s("─", "-")
	CornerTopRight    = s("╮", "+")
	ConnectLeft       = s("├", "+")
	CornerBottomRight = s("╯", "+")

	Info    = s("●", "•")
	Success = s("◆", "*")
	Warn    = s("▲", "!")
	Error   = s("■", "x")
)

// State returns the glyph for a prompt state.
func State(state core.State) string {
	switch state {
	case core.ErrorState:
		return StepError
	case core.CancelState:
		return StepCancel
	case core.SubmitState:
		return StepSubmit
	default:
		return StepActive
	}
}
