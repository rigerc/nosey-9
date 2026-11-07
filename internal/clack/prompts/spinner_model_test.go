package prompts

import (
	"strings"
	"testing"
	"time"
)

func TestSpinnerBubbleTeaTickAndFinalFrame(t *testing.T) {
	t.Setenv("CI", "false")
	s := Spinner(SpinnerOptions{
		Frames: []string{"A", "B"}, FrameInterval: time.Millisecond,
		Indicator: SpinnerTimerIndicator,
	})
	s.isCI = false
	started := time.Now().Add(-2 * time.Second)
	m := &spinnerModel{controller: s, message: "Loading", started: started}
	if !strings.Contains(m.View().Content, "A Loading [0s]") {
		t.Fatalf("initial view: %q", m.View().Content)
	}
	m.Update(spinnerTickMsg(started.Add(2 * time.Second)))
	if !strings.Contains(m.View().Content, "B Loading [2s]") {
		t.Fatalf("ticked view: %q", m.View().Content)
	}
	m.Update(spinnerMessageMsg("Updated..."))
	if !strings.Contains(m.View().Content, "Updated [2s]") {
		t.Fatalf("update view: %q", m.View().Content)
	}
	m.Update(spinnerStopMsg{text: "Complete", code: 0})
	if !strings.Contains(m.View().Content, "Complete") || !m.finished {
		t.Fatalf("final view: %q", m.View().Content)
	}
}
