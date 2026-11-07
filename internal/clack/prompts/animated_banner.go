package prompts

import (
	"errors"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/banner"
	"golang.org/x/term"
)

// BannerAnimationOptions configures the effect and timing of an inline banner.
type BannerAnimationOptions struct {
	// FrameInterval is the delay between frames. Defaults to 75ms.
	FrameInterval time.Duration
	// Effect selects rows (default), wipe, gradient-sweep, or fade.
	Effect BannerEffect
}

type bannerTickMsg struct{}

type animatedBannerModel struct {
	lines    []string
	visible  int
	interval time.Duration
	effect   BannerEffect
	palette  []string
}

func bannerTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return bannerTickMsg{} })
}

func (m *animatedBannerModel) Init() tea.Cmd {
	if m.complete() {
		return tea.Quit
	}
	return bannerTick(m.interval)
}

func (m *animatedBannerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case bannerTickMsg:
		if !m.advance() {
			return m, tea.Quit
		}
		return m, bannerTick(m.interval)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter", "space":
			m.visible = m.totalFrames()
			return m, tea.Quit
		case "esc", "ctrl+c":
			return m, tea.Interrupt
		}
	}
	return m, nil
}

func (m *animatedBannerModel) View() tea.View {
	// Every effect reserves the full banner height from the first frame.
	return tea.NewView(m.frame())
}

// AnimatedBanner animates FIGlet art in the normal terminal buffer using
// the selected effect (row reveal by default). The final frame remains in
// scrollback. Enter/space skips; Esc/Ctrl+C cancels.
//
// Non-terminal/CI output and calls made inside another Bubble Tea program
// use the existing static Banner behavior instead of emitting animation frames
// or launching a nested program.
func AnimatedBanner(text string, options BannerOptions, animation BannerAnimationOptions) error {
	if err := validateBannerEffect(animation.Effect); err != nil {
		return err
	}
	out := options.Output
	if out == nil {
		out = os.Stdout
	}
	file, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) || os.Getenv("CI") != "" || os.Getenv("TERM") == "dumb" || core.HasActiveProgram() || options.Config.Parser == "html" {
		return Banner(text, options)
	}

	cfg := options.Config
	cfg.Text = text
	if cfg.Width == 0 {
		if width, _, err := term.GetSize(int(file.Fd())); err == nil && width >= 20 {
			cfg.Width = width
		}
	}
	art, err := banner.Render(cfg)
	if err != nil {
		return err
	}
	model, err := newAnimatedBannerModel(art, animation, false)
	if err != nil {
		return err
	}
	if model.complete() {
		return core.WriteStyled(out, strings.TrimRight(art, "\r\n")+"\r\n")
	}
	programOptions := []tea.ProgramOption{tea.WithOutput(out)}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		programOptions = append(programOptions, tea.WithInput(os.Stdin))
	} else {
		programOptions = append(programOptions, tea.WithInput(nil))
	}
	program := tea.NewProgram(model, programOptions...)
	core.BeginProgram(program, out)
	defer core.EndProgram(program)

	_, err = program.Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return core.ErrCancelPrompt
	}
	return err
}
