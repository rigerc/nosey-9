package prompts_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/stretchr/testify/assert"
)

func TestTasksExecuteSequentially(t *testing.T) {
	var completed []string
	w := &MockWriter{}
	prompts.Tasks([]prompts.Task{
		{Title: "Foo", Task: func(func(string)) (string, error) { completed = append(completed, "Foo"); return "Foo done", nil }},
		{Title: "Bar", Task: func(func(string)) (string, error) { completed = append(completed, "Bar"); return "Bar done", nil }},
		{Title: "Baz", Task: func(func(string)) (string, error) { completed = append(completed, "Baz"); return "Baz done", nil }},
	}, prompts.SpinnerOptions{Output: w, FrameInterval: time.Millisecond})
	assert.Equal(t, []string{"Foo", "Bar", "Baz"}, completed)
	output := strings.Join(w.Data, "")
	for _, expected := range []string{"Foo done", "Bar done", "Baz done"} {
		assert.Contains(t, output, expected)
	}
}

func TestTasksUpdateMessage(t *testing.T) {
	w := &MockWriter{}
	prompts.Tasks([]prompts.Task{{Title: "Foo", Task: func(message func(string)) (string, error) { message("Bar"); return "", nil }}}, prompts.SpinnerOptions{Output: w, FrameInterval: time.Millisecond})
	assert.Contains(t, strings.Join(w.Data, ""), "Bar")
}

func TestTasksDisabled(t *testing.T) {
	counter := 0
	prompts.Tasks([]prompts.Task{{Title: "Skip", Disabled: true, Task: func(func(string)) (string, error) { counter++; return "", nil }}}, prompts.SpinnerOptions{Output: &MockWriter{}})
	assert.Equal(t, 0, counter)
}

func TestTasksError(t *testing.T) {
	w := &MockWriter{}
	prompts.Tasks([]prompts.Task{{Title: "Foo", Task: func(func(string)) (string, error) { return "", errors.New("task error") }}}, prompts.SpinnerOptions{Output: w})
	assert.Contains(t, strings.Join(w.Data, ""), "task error")
}
