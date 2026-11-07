package prompts

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-cli-template/internal/clack/prompts/banner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnimatedBannerModelRevealsRows(t *testing.T) {
	m := &animatedBannerModel{
		lines:    []string{"first", "second", "third"},
		visible:  1,
		interval: time.Millisecond,
	}
	require.NotNil(t, m.Init())
	assert.Equal(t, "first\n\n", m.View().Content)

	_, cmd := m.Update(bannerTickMsg{})
	require.NotNil(t, cmd)
	assert.Equal(t, "first\nsecond\n", m.View().Content)

	_, cmd = m.Update(bannerTickMsg{})
	require.NotNil(t, cmd)
	assert.Equal(t, "first\nsecond\nthird", m.View().Content)
	assert.IsType(t, tea.Quit(), cmd())
}

func TestAnimatedBannerModelSkipAndCancel(t *testing.T) {
	lines := []string{"one", "two", "three"}
	m := &animatedBannerModel{lines: lines, visible: 1, interval: time.Millisecond}
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.Quit(), cmd())
	assert.Equal(t, strings.Join(lines, "\n"), m.View().Content)

	m = &animatedBannerModel{lines: lines, visible: 1, interval: time.Millisecond}
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.Interrupt(), cmd())
	assert.Equal(t, 1, m.visible)
}

func TestAnimatedBannerPreservesANSI(t *testing.T) {
	m := &animatedBannerModel{lines: []string{"\x1b[31mred\x1b[0m", "plain"}, visible: 1}
	assert.Equal(t, "\x1b[31mred\x1b[0m\n", m.View().Content)
}

func TestAnimatedBannerFallsBackForNonTerminalOutput(t *testing.T) {
	cfg := banner.Config{Font: "standard", Parser: "terminal"}
	var want, got bytes.Buffer
	require.NoError(t, Banner("Hello", BannerOptions{Config: cfg, Output: &want}))
	require.NoError(t, AnimatedBanner("Hello", BannerOptions{Config: cfg, Output: &got}, BannerAnimationOptions{FrameInterval: time.Millisecond}))
	assert.Equal(t, want.String(), got.String())
}

func TestAnimatedBannerPropagatesRenderError(t *testing.T) {
	var out bytes.Buffer
	err := AnimatedBanner("Hello", BannerOptions{
		Config: banner.Config{Font: "standard", Color: "not-a-color"},
		Output: &out,
	}, BannerAnimationOptions{})
	require.Error(t, err)
	assert.Empty(t, out.String())
}

func TestAnimatedBannerRejectsUnknownEffectOnNonTerminalOutput(t *testing.T) {
	var out bytes.Buffer
	err := AnimatedBanner("Hello", BannerOptions{
		Config: banner.Config{Font: "standard", Parser: "terminal"},
		Output: &out,
	}, BannerAnimationOptions{Effect: BannerEffect("invalid")})
	require.ErrorContains(t, err, "unknown banner animation effect")
	assert.Empty(t, out.String())
}
