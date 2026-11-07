package prompts

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// BannerEffect selects how FIGlet artwork appears in the inline terminal.
// An empty effect is equivalent to BannerEffectRows for compatibility.
type BannerEffect string

const (
	// BannerEffectRows reveals one full row at a time (the default).
	BannerEffectRows BannerEffect = "rows"
	// BannerEffectWipe reveals each row from left to right by terminal cell.
	BannerEffectWipe BannerEffect = "wipe"
	// BannerEffectGradientSweep moves the banner's rendered colors across its
	// characters, then restores the exact original artwork.
	BannerEffectGradientSweep BannerEffect = "gradient-sweep"
	// BannerEffectFade gradually reveals glyphs in a stable dither pattern,
	// without relying on a particular terminal background color.
	BannerEffectFade BannerEffect = "fade"
)

const (
	bannerSweepFrames = 12
	bannerFadeFrames  = 8
	bannerWipeFrames  = 16
)

var bannerRGBPattern = regexp.MustCompile("\x1b\\[38;2;[0-9]+;[0-9]+;[0-9]+m")

func validateBannerEffect(effect BannerEffect) error {
	switch effect {
	case "", BannerEffectRows, BannerEffectWipe, BannerEffectGradientSweep, BannerEffectFade:
		return nil
	default:
		return fmt.Errorf("go-clack: unknown banner animation effect %q", effect)
	}
}

// newAnimatedBannerModel reuses the same previously-generated FIGlet artwork
// for every effect. Re-rendering figlet on each tick can change random fonts
// and gradients and also costs more work than pure frame composition.
func newAnimatedBannerModel(art string, animation BannerAnimationOptions, static bool) (*animatedBannerModel, error) {
	if err := validateBannerEffect(animation.Effect); err != nil {
		return nil, err
	}

	normalized := strings.TrimRight(strings.ReplaceAll(art, "\r\n", "\n"), "\n")
	interval := animation.FrameInterval
	if interval <= 0 {
		interval = 75 * time.Millisecond
	}

	m := &animatedBannerModel{
		lines:    strings.Split(normalized, "\n"),
		visible:  1,
		interval: interval,
		effect:   animation.Effect,
		palette:  bannerPalette(art),
	}
	if static {
		m.visible = m.totalFrames()
	}
	return m, nil
}

// bannerPalette retains the actual TrueColor stops in the rendered FIGlet
// output. This also supports randomly selected gradients without choosing
// a second random gradient during animation.
func bannerPalette(art string) []string {
	found := bannerRGBPattern.FindAllString(art, -1)
	seen := make(map[string]bool)
	result := make([]string, 0, 12)
	for _, color := range found {
		if !seen[color] {
			result = append(result, color)
			seen[color] = true
			if len(result) >= 12 {
				break
			}
		}
	}
	return result
}

func (m *animatedBannerModel) maxColumns() int {
	width := 0
	for _, line := range m.lines {
		width = max(width, ansi.StringWidth(line))
	}
	return width
}

func (m *animatedBannerModel) totalFrames() int {
	switch m.effect {
	case BannerEffectWipe:
		return max(1, min(bannerWipeFrames, m.maxColumns()))
	case BannerEffectGradientSweep:
		return bannerSweepFrames
	case BannerEffectFade:
		return bannerFadeFrames
	default:
		return max(1, len(m.lines))
	}
}

func (m *animatedBannerModel) complete() bool {
	return m.visible >= m.totalFrames()
}

// advance returns whether an additional tick is necessary.
func (m *animatedBannerModel) advance() bool {
	if m.complete() {
		return false
	}
	m.visible++
	return !m.complete()
}

func (m *animatedBannerModel) frame() string {
	// Never reconstruct the final frame: retain all original SGR styling,
	// whitespace, and grapheme clusters from figlet-go exactly.
	if m.complete() {
		return strings.Join(m.lines, "\n")
	}

	frame := make([]string, len(m.lines))
	switch m.effect {
	case BannerEffectWipe:
		limit := (m.visible*m.maxColumns() + m.totalFrames() - 1) / m.totalFrames()
		for i, line := range m.lines {
			frame[i] = ansi.Truncate(line, limit, "")
			if ansi.StringWidth(line) > limit && strings.Contains(frame[i], "\x1b[") {
				// A truncated line may have lost the original SGR reset.
				frame[i] += "\x1b[0m"
			}
		}
	case BannerEffectGradientSweep:
		if len(m.palette) == 0 {
			// Plain or ANSI-16 artwork cannot supply RGB stops. A simple
			// wipe retains its plain-text contract in that case.
			limit := (m.visible*m.maxColumns() + m.totalFrames() - 1) / m.totalFrames()
			for i, line := range m.lines {
				frame[i] = ansi.Truncate(line, limit, "")
				if ansi.StringWidth(line) > limit && strings.Contains(frame[i], "\x1b[") {
					frame[i] += "\x1b[0m"
				}
			}
		} else {
			for i, line := range m.lines {
				frame[i] = m.sweepLine(line)
			}
		}
	case BannerEffectFade:
		for i, line := range m.lines {
			frame[i] = m.fadeLine(line, i)
		}
	default:
		copy(frame, m.lines[:min(m.visible, len(m.lines))])
	}
	return strings.Join(frame, "\n")
}

func (m *animatedBannerModel) sweepLine(line string) string {
	var out strings.Builder
	column := 0
	width := max(1, m.maxColumns())
	for _, r := range ansi.Strip(line) {
		cells := ansi.StringWidth(string(r))
		if !unicode.IsSpace(r) && cells > 0 {
			stop := (column*len(m.palette)/width + m.visible - 1) % len(m.palette)
			out.WriteString(m.palette[stop])
		}
		out.WriteRune(r)
		column += cells
	}
	out.WriteString("\x1b[0m")
	return out.String()
}

func (m *animatedBannerModel) fadeLine(line string, row int) string {
	var out strings.Builder
	column := 0
	keepCombining := false
	for _, r := range ansi.Strip(line) {
		cells := ansi.StringWidth(string(r))
		if cells == 0 {
			if keepCombining {
				out.WriteRune(r)
			}
			continue
		}
		if unicode.IsSpace(r) {
			out.WriteRune(r)
			keepCombining = true
		} else {
			keepCombining = ((column*7 + row*11) % bannerFadeFrames) < m.visible
			if keepCombining {
				out.WriteRune(r)
			} else {
				out.WriteString(strings.Repeat(" ", cells))
			}
		}
		column += cells
	}
	return out.String()
}
