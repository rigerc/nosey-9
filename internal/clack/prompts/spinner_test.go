package prompts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

// The Bubble Tea renderer may coalesce ticks, so tests intentionally assert
// public outcomes rather than individual terminal frame writes.
func TestSpinnerStartStop(t *testing.T) {
	w := &MockWriter{}
	s := prompts.Spinner(prompts.SpinnerOptions{Output: w, FrameInterval: time.Millisecond})
	s.Start("Loading")
	s.Stop("Loaded", 0)
	assert.Contains(t, strings.Join(w.Data, ""), "Loaded")
}

func TestSpinnerMessageAndFinalStatus(t *testing.T) {
	w := &MockWriter{}
	s := prompts.Spinner(prompts.SpinnerOptions{Output: w, FrameInterval: time.Millisecond})
	s.Start("Loading...")
	s.Message("Still Loading...")
	s.Stop("", 0)
	assert.Contains(t, strings.Join(w.Data, ""), "Still Loading")
}

func TestSpinnerErrorStatus(t *testing.T) {
	w := &MockWriter{}
	s := prompts.Spinner(prompts.SpinnerOptions{Output: w})
	s.Start("Loading")
	s.Stop("failed", 2)
	assert.Contains(t, strings.Join(w.Data, ""), "failed")
}
