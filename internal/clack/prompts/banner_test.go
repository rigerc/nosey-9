package prompts_test

import (
	"bytes"
	"testing"

	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/go-cli-template/internal/clack/prompts/banner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBannerWritesToOutput(t *testing.T) {
	var out bytes.Buffer
	err := prompts.Banner("Hello", prompts.BannerOptions{
		Config: banner.Config{Font: "standard", Parser: "terminal"},
		Output: &out,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.String())
	assert.Contains(t, out.String(), "\r\n")
}

func TestBannerComponent(t *testing.T) {
	component, err := prompts.BannerComponent(banner.Config{
		Text: "Hello", Font: "standard", Parser: "terminal",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, component())
	assert.Equal(t, component(), component())
}
