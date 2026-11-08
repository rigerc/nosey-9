package core_test

import (
	"os"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runPersistentTestPrompt(t *testing.T, output *os.File, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	_, err = w.Write([]byte("\r"))
	require.NoError(t, err)

	p := core.NewPrompt(core.PromptParams[string]{
		Input: r, Output: output,
		Render: func(*core.Prompt[string]) string { return text },
	})
	_, err = p.Run()
	require.NoError(t, err)
	assert.True(t, strings.Contains(p.Frame, "HEADER"))
}

func TestPersistentComponentsAcrossForms(t *testing.T) {
	// Bubble Tea manages terminal modes and redraws the active prompt.
	core.SetPersistent(func() string { return "HEADER" })
	t.Cleanup(core.ClearPersistent)

	output, err := os.CreateTemp(t.TempDir(), "terminal")
	require.NoError(t, err)
	defer func() { _ = output.Close() }()

	runPersistentTestPrompt(t, output, "Form one")
	runPersistentTestPrompt(t, output, "Form two")
	content, err := os.ReadFile(output.Name())
	require.NoError(t, err)
	assert.Contains(t, string(content), "HEADER")
	assert.Contains(t, string(content), "Form two")
	// Screen clearing is now implemented by Bubble Tea. Avoid coupling tests
	// to a particular ANSI sequence or the number of interim redraws.

	core.ClearPersistent()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	_, err = w.Write([]byte("\r"))
	require.NoError(t, err)
	p := core.NewPrompt(core.PromptParams[string]{
		Input: r, Output: output,
		Render: func(*core.Prompt[string]) string { return "No header" },
	})
	_, err = p.Run()
	require.NoError(t, err)
	assert.Equal(t, "No header", p.Frame)
}
