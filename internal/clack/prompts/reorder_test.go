package prompts_test

import (
	tea "charm.land/bubbletea/v2"
	"os"
	"testing"

	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReorderReturnsValuesInOrder(t *testing.T) {
	input, send, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = input.Close() }()
	defer func() { _ = send.Close() }()
	_, err = send.Write([]byte("\r"))
	require.NoError(t, err)

	output, err := os.CreateTemp(t.TempDir(), "reorder")
	require.NoError(t, err)
	defer func() { _ = output.Close() }()

	result, err := prompts.Reorder(prompts.ReorderParams[string]{
		Input:   input,
		Output:  output,
		Message: "Choose order:",
		Options: []*prompts.ReorderOption[string]{
			{Label: "First", Value: "1"},
			{Label: "Second", Value: "2"},
			{Label: "Third", Value: "3"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3"}, result)
}

func TestReorderRequiresOptions(t *testing.T) {
	assert.Panics(t, func() {
		_, _ = prompts.Reorder(prompts.ReorderParams[string]{Message: "Empty"})
	})
}

func TestReorderKeyboardMovesPickedItem(t *testing.T) {
	p := prompts.ReorderModel(prompts.ReorderParams[string]{
		Message: "Choose order:",
		Options: []*prompts.ReorderOption[string]{
			{Label: "First", Value: "1"},
			{Label: "Second", Value: "2"},
			{Label: "Third", Value: "3"},
		},
	})
	tm := startTestPrompt(t, p, "Choose order:")
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyDown})
	sendPromptKey(tm, tea.Key{Code: ' '})
	sendPromptKey(tm, tea.Key{Code: tea.KeyEnter})
	finishTestPrompt(t, tm)

	assert.Equal(t, []string{"2", "1", "3"}, p.Value)
}
