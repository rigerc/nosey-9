package banner_test

import (
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/clack/prompts/banner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlainBanner(t *testing.T) {
	out, err := banner.Render(banner.Config{
		Text: "Hi", Font: "standard", Parser: "terminal", Color: "cyan",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out)
	assert.NotContains(t, out, "\x1b[")
	assert.NotEqual(t, "Hi", strings.TrimSpace(out))
}

func TestBannerInvalidColor(t *testing.T) {
	_, err := banner.Render(banner.Config{Text: "X", Font: "standard", Color: "not-a-color"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognised color")
}

func TestBannerConflictingColorSources(t *testing.T) {
	_, err := banner.Render(banner.Config{
		Text: "X", Font: "standard", Color: "red", RandomColor: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestBannerCustomGradient(t *testing.T) {
	gradient := banner.Gradient{Name: "custom", Colors: []string{"FF0000", "0000FF"}}
	out, err := banner.Render(banner.Config{
		Text: "Hi", Font: "standard", Parser: "terminal-color", Gradient: &gradient,
	})
	require.NoError(t, err)
	assert.Contains(t, out, "\x1b[38;2;")
}

func TestBannerInvalidGradientStop(t *testing.T) {
	gradient := banner.Gradient{Name: "bad", Colors: []string{"not-hex"}}
	_, err := banner.Render(banner.Config{
		Text: "X", Font: "standard", Gradient: &gradient,
	})
	require.Error(t, err)
}
