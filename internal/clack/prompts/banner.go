// Package prompts provides styled, native Bubble Tea prompts and messages.
package prompts

import (
	"github.com/rigerc/nosey-9/internal/clack/core"
	"io"
	"os"
	"strings"

	"github.com/rigerc/nosey-9/internal/clack/prompts/banner"
)

// BannerOptions configures a printable FIGlet banner.
type BannerOptions struct {
	Config banner.Config
	Output io.Writer
}

// Banner writes an ASCII-art heading to the configured output (stdout by default).
// To keep the banner visible above subsequent forms, use BannerComponent with SetPersistent.
func Banner(text string, options BannerOptions) error {
	cfg := options.Config
	cfg.Text = text
	art, err := banner.Render(cfg)
	if err != nil {
		return err
	}
	out := options.Output
	if out == nil {
		out = os.Stdout
	}
	return core.WriteStyled(out, strings.TrimRight(art, "\r\n")+"\r\n")
}

// BannerComponent renders a banner once as a reusable persistent component.
// Rendering it once also ensures randomly selected fonts and gradients remain stable.
func BannerComponent(cfg banner.Config) (PersistentComponent, error) {
	art, err := banner.Render(cfg)
	if err != nil {
		return nil, err
	}
	return StaticComponent(art), nil
}
