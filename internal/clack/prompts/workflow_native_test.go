package prompts

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts/symbols"
)

// CONTRACT: nativeText exercises the ComposablePrompt interface accepted by ModelStep.
func nativeText(prompt string) core.ComposablePrompt {
	p := core.NewPrompt(core.PromptParams[string]{
		Render: func(p *core.Prompt[string]) string { return prompt + ": " + p.Value },
	})
	p.HandleKey = func(key *core.Key) {
		p.Value, p.CursorIndex = p.TrackKeyValue(key, p.Value, p.CursorIndex)
	}
	return p
}

func startWorkflowTest(t *testing.T, m *workflowModel, want string) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })
	waitForWorkflowOutput(t, tm, want)
	return tm
}

func waitForWorkflowOutput(t *testing.T, tm *teatest.TestModel, want string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(want))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(10*time.Millisecond))
}

func finishWorkflowTest(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

func TestComposedWorkflowSingleBubbleTeaModel(t *testing.T) {
	var r struct {
		First  string
		Second string
	}
	workflow := Workflow(&r).
		ModelStep("First", func() core.ComposablePrompt { return nativeText("first") }).
		ModelStep("Second", func() core.ComposablePrompt { return nativeText("second") })
	model := &workflowModel{builder: workflow, steps: workflow.steps}
	tm := startWorkflowTest(t, model, "first:")

	tm.Type("A")
	tm.Send(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	waitForWorkflowOutput(t, tm, "second:")
	tm.Type("B")
	tm.Send(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	finishWorkflowTest(t, tm)

	if r.First != "A" || r.Second != "B" || !model.done {
		t.Fatalf("results=%+v finished=%v", r, model.done)
	}
	if !strings.Contains(model.View().Content, "first: A") || !strings.Contains(model.View().Content, "second: B") {
		t.Fatalf("history lost: %q", model.View().Content)
	}
}

func TestComposedWorkflowConditionalAndCancel(t *testing.T) {
	var r struct {
		First   string
		Skipped string
		Last    string
	}
	count := 0
	workflow := Workflow(&r).
		ModelStep("First", func() core.ComposablePrompt { return nativeText("first") }).
		ConditionalModelStep("Skipped", func() bool { return false }, func() core.ComposablePrompt { t.Fatal("skipped model created"); return nil }).
		ModelStep("Last", func() core.ComposablePrompt { return nativeText("last") }).
		OnCancel(func(name string, err error) {
			if name != "Last" || err != core.ErrCancelPrompt {
				t.Errorf("wrong cancellation: %s %v", name, err)
			}
			count++
		})
	m := &workflowModel{builder: workflow, steps: workflow.steps}
	tm := startWorkflowTest(t, m, "first:")
	tm.Send(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	waitForWorkflowOutput(t, tm, "last:")
	tm.Send(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	finishWorkflowTest(t, tm)

	if m.index != 2 || m.err != core.ErrCancelPrompt || count != 1 || !m.done {
		t.Fatalf("cancel: index=%d err=%v count=%d done=%v", m.index, m.err, count, m.done)
	}
}

func TestGoClackStyledNativeModel(t *testing.T) {
	p := TextModel(TextParams{Message: "Your name?", Placeholder: "Ada"})
	p.Init()
	frame := p.View().Content
	if !strings.Contains(frame, "Your name?") || !strings.Contains(frame, "Ada") || !strings.Contains(frame, symbols.Bar) {
		t.Fatalf("go-clack style not preserved: %q", frame)
	}
	var _ core.ComposablePrompt = p
	var _ tea.Model = p
}

func TestRunNativeRejectsLegacyCallbacks(t *testing.T) {
	var r struct{ First string }
	// CONTRACT: workflow steps return heterogeneous values.
	err := Workflow(&r).Step("First", func() (any, error) { return "a", nil }).RunNative()
	if err == nil {
		t.Fatal("nested callback should not be accepted by RunNative")
	}
}
