package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBannerEffectsCompleteWithOriginalArtwork(t *testing.T) {
	art := "\x1b[38;2;255;0;0mHELLO\x1b[0m\n\x1b[38;2;0;0;255mWORLD\x1b[0m"
	for _, effect := range []BannerEffect{
		BannerEffectRows,
		BannerEffectWipe,
		BannerEffectGradientSweep,
		BannerEffectFade,
	} {
		t.Run(string(effect), func(t *testing.T) {
			m, err := newAnimatedBannerModel(art, BannerAnimationOptions{
				FrameInterval: time.Millisecond,
				Effect:        effect,
			}, false)
			require.NoError(t, err)
			assert.Equal(t, 2, len(m.lines))
			require.Greater(t, m.totalFrames(), 1)
			assert.False(t, m.complete())
			assert.Equal(t, time.Millisecond, m.interval)
			for !m.complete() {
				assert.True(t, m.visible < m.totalFrames())
				m.advance()
			}
			assert.Equal(t, art, m.frame())
			assert.False(t, m.advance())

			static, err := newAnimatedBannerModel(art, BannerAnimationOptions{Effect: effect}, true)
			require.NoError(t, err)
			assert.True(t, static.complete())
			assert.Equal(t, art, static.frame())
		})
	}
}

func TestBannerWipeUsesDisplayCellsNotBytes(t *testing.T) {
	art := "\x1b[38;2;255;0;0mA界B\x1b[0m\n1234"
	m, err := newAnimatedBannerModel(art, BannerAnimationOptions{Effect: BannerEffectWipe}, false)
	require.NoError(t, err)
	assert.Equal(t, 4, m.maxColumns())
	assert.Equal(t, 4, m.totalFrames())
	for !m.complete() {
		lines := strings.Split(m.frame(), "\n")
		require.Len(t, lines, 2)
		assert.LessOrEqual(t, ansi.StringWidth(lines[0]), m.visible)
		assert.LessOrEqual(t, ansi.StringWidth(lines[1]), m.visible)
		assert.NotContains(t, lines[0], "\ufffd")
		// Styles and grapheme widths are checked as terminal cells,
		// rather than asserting the number of ANSI bytes.
		m.advance()
	}
	assert.Equal(t, art, m.frame())
}

func TestBannerGradientSweepUsesRenderedPalette(t *testing.T) {
	art := "\x1b[38;2;255;0;0mAB\x1b[0m\x1b[38;2;0;0;255mCD\x1b[0m\n EFG"
	m, err := newAnimatedBannerModel(art, BannerAnimationOptions{Effect: BannerEffectGradientSweep}, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"\x1b[38;2;255;0;0m", "\x1b[38;2;0;0;255m"}, m.palette)
	first := m.frame()
	assert.Equal(t, ansi.Strip(art), ansi.Strip(first))
	assert.Contains(t, first, m.palette[0])
	assert.Contains(t, first, m.palette[1])
	m.advance()
	assert.NotEqual(t, first, m.frame())
	for !m.complete() {
		m.advance()
	}
	assert.Equal(t, art, m.frame())
}

func TestBannerGradientSweepPlainTextFallback(t *testing.T) {
	art := "ABCDEF\n123456"
	m, err := newAnimatedBannerModel(art, BannerAnimationOptions{Effect: BannerEffectGradientSweep}, false)
	require.NoError(t, err)
	assert.Empty(t, m.palette)
	assert.NotContains(t, m.frame(), "\x1b[")
	assert.Less(t, ansi.StringWidth(strings.Split(m.frame(), "\n")[0]), 6)
	for !m.complete() {
		m.advance()
	}
	assert.Equal(t, art, m.frame())
}

func TestBannerFadeIsProgressiveAndTerminalNeutral(t *testing.T) {
	art := "\x1b[38;2;200;100;50mABCDEFGHIJKLMN\x1b[0m\n0123456789"
	m, err := newAnimatedBannerModel(art, BannerAnimationOptions{Effect: BannerEffectFade}, false)
	require.NoError(t, err)
	lastCount := 0
	for !m.complete() {
		frame := m.frame()
		assert.NotContains(t, frame, "\x1b[")
		count := len(strings.ReplaceAll(strings.ReplaceAll(frame, " ", ""), "\n", ""))
		assert.GreaterOrEqual(t, count, lastCount)
		lastCount = count
		m.advance()
	}
	assert.Equal(t, art, m.frame())
}

func TestBannerUnknownEffect(t *testing.T) {
	_, err := newAnimatedBannerModel("ABC", BannerAnimationOptions{Effect: BannerEffect("not-supported")}, false)
	require.ErrorContains(t, err, "unknown banner animation effect")
}

func TestBannerWipeOneLineStillAnimates(t *testing.T) {
	m, err := newAnimatedBannerModel("SINGLE LINE", BannerAnimationOptions{Effect: BannerEffectWipe}, false)
	require.NoError(t, err)
	assert.False(t, m.complete())
	assert.Less(t, ansi.StringWidth(m.frame()), ansi.StringWidth("SINGLE LINE"))
	for !m.complete() {
		m.advance()
	}
	assert.Equal(t, "SINGLE LINE", m.frame())
}

func TestBannerFadeRetainsWideCellWidth(t *testing.T) {
	art := "A界B\n界 C"
	m, err := newAnimatedBannerModel(art, BannerAnimationOptions{Effect: BannerEffectFade}, false)
	require.NoError(t, err)
	for !m.complete() {
		lines := strings.Split(m.frame(), "\n")
		require.Len(t, lines, 2)
		assert.Equal(t, 4, ansi.StringWidth(lines[0]))
		assert.Equal(t, 4, ansi.StringWidth(lines[1]))
		m.advance()
	}
	assert.Equal(t, art, m.frame())
}
