package prompts

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts/banner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowAnimatedBannerTicksWithoutBlockingPrompt(t *testing.T) {
	var result struct{ Name string }
	workflow := Workflow(&result).
		WithAnimatedBanner(banner.Config{Text: "HELLO"}, BannerAnimationOptions{FrameInterval: time.Millisecond}).
		ModelStep("Name", func() core.ComposablePrompt { return nativeText("name") })
	m := &workflowModel{
		builder: workflow,
		steps:   workflow.steps,
		banner: &animatedBannerModel{
			lines:    []string{"banner one", "banner two", "banner three"},
			visible:  1,
			interval: time.Millisecond,
		},
	}
	require.NotNil(t, m.Init())
	assert.Contains(t, m.View().Content, "banner one")
	assert.Contains(t, m.View().Content, "name:")

	_, cmd := m.Update(bannerTickMsg{})
	require.NotNil(t, cmd)
	assert.Equal(t, 2, m.banner.visible)
	assert.Contains(t, m.View().Content, "banner two")
	assert.Equal(t, 0, m.index)

	// Key input is handled by the active form, even while the header animates.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'A', Text: "A"}))
	assert.Contains(t, m.View().Content, "name: A")
	_, cmd = m.Update(bannerTickMsg{})
	assert.Nil(t, cmd)
	assert.Equal(t, 3, m.banner.visible)

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	assert.True(t, m.done)
	assert.Equal(t, "A", result.Name)
	assert.Contains(t, m.View().Content, "banner three")
	assert.Contains(t, m.View().Content, "name: A")
}

func TestWorkflowAnimatedBannerStaticFallback(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "output-*.txt")
	require.NoError(t, err)
	defer func() { _ = out.Close() }()
	w := Workflow(&struct{}{}).WithAnimatedBanner(
		banner.Config{Text: "Hi", Font: "standard", Parser: "terminal"},
		BannerAnimationOptions{},
	)
	m, err := w.prepareAnimatedBanner(out)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Greater(t, len(m.lines), 1)
	assert.Equal(t, len(m.lines), m.visible)
	assert.Equal(t, 75*time.Millisecond, m.interval)
	assert.NotEmpty(t, strings.TrimSpace(m.View().Content))
}

func TestWorkflowAnimatedBannerInvalidConfig(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "output-*.txt")
	require.NoError(t, err)
	defer func() { _ = out.Close() }()
	w := Workflow(&struct{}{}).WithAnimatedBanner(
		banner.Config{Text: "X", Font: "standard", Color: "invalid-color"},
		BannerAnimationOptions{},
	)
	_, err = w.prepareAnimatedBanner(out)
	require.Error(t, err)

	w.WithAnimatedBanner(banner.Config{Text: "X", Parser: "html"}, BannerAnimationOptions{})
	_, err = w.prepareAnimatedBanner(out)
	require.ErrorContains(t, err, "terminal output")
}

func TestWorkflowGradientSweepLeavesPromptInputResponsive(t *testing.T) {
	art := "\x1b[38;2;255;0;0mAB\x1b[0m\x1b[38;2;0;255;0mCD\x1b[0m\nEFGH"
	bannerModel, err := newAnimatedBannerModel(art, BannerAnimationOptions{
		Effect:        BannerEffectGradientSweep,
		FrameInterval: time.Millisecond,
	}, false)
	require.NoError(t, err)

	var result struct{ Name string }
	w := Workflow(&result).ModelStep("Name", func() core.ComposablePrompt {
		return nativeText("name")
	})
	m := &workflowModel{builder: w, steps: w.steps, banner: bannerModel}
	require.NotNil(t, m.Init())
	startFrame := m.View().Content

	_, cmd := m.Update(bannerTickMsg{})
	require.NotNil(t, cmd)
	assert.NotEqual(t, startFrame, m.View().Content)

	// The input belongs to the child model, not the animated header.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'A', Text: "A"}))
	assert.Contains(t, m.View().Content, "name: A")
	assert.Equal(t, 0, m.index)

	for !bannerModel.complete() {
		m.Update(bannerTickMsg{})
	}
	assert.Contains(t, m.View().Content, art)
	assert.Contains(t, m.View().Content, "name: A")

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	assert.True(t, m.done)
	assert.Equal(t, "A", result.Name)
	assert.Contains(t, m.View().Content, art)
}

func TestWorkflowBannerEffectRejectsUnknownEffect(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "output-*.txt")
	require.NoError(t, err)
	defer func() { _ = out.Close() }()

	w := Workflow(&struct{}{}).WithAnimatedBanner(
		banner.Config{Text: "Hello", Font: "standard"},
		BannerAnimationOptions{Effect: BannerEffect("unsupported")},
	)
	_, err = w.prepareAnimatedBanner(out)
	require.ErrorContains(t, err, "unknown banner animation effect")
}
