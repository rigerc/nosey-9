package theme_test

import (
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts/symbols"
	"github.com/rigerc/nosey-9/internal/clack/prompts/theme"
	"github.com/stretchr/testify/assert"
)

func TestApplyThemeInitialState(t *testing.T) {
	testCases := []struct {
		Description     string
		ValueWithCursor string
		Placeholder     string
		Expected        string
	}{
		{
			Description: "InitialState",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description: "InitialStateWithPlaceholder",
			Placeholder: "Placeholder",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Placeholder",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description:     "InitialStateWithValue",
			ValueWithCursor: "Value with cursor█",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Value with cursor█",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description:     "InitialStateWithPlaceholderAndValue",
			Placeholder:     "Placeholder",
			ValueWithCursor: "Value with cursor█",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Value with cursor█",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description:     "InitialStateWithPlaceholderAndCursor",
			Placeholder:     "Placeholder",
			ValueWithCursor: "█",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Placeholder",
				symbols.BarEnd,
			}, "\r\n"),
		},
	}
	for _, tC := range testCases {
		t.Run(tC.Description, func(t *testing.T) {
			frame := theme.ApplyTheme(theme.Params[string]{
				Context: core.Prompt[string]{
					State: core.InitialState,
				},
				Message:         "Test message",
				ValueWithCursor: tC.ValueWithCursor,
				Placeholder:     tC.Placeholder,
			})
			assert.Equal(t, tC.Expected, frame)
		})
	}
}

func TestApplyThemeActiveState(t *testing.T) {
	testCases := []struct {
		Description     string
		ValueWithCursor string
		Placeholder     string
		Expected        string
	}{
		{
			Description: "ActiveState",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description: "ActiveStateWithPlaceholder",
			Placeholder: "Placeholder",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Placeholder",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description:     "ActiveStateWithValue",
			ValueWithCursor: "Value with cursor ",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Value with cursor ",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description:     "ActiveStateWithPlaceholderAndValue",
			Placeholder:     "Placeholder",
			ValueWithCursor: "Value with cursor ",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Value with cursor ",
				symbols.BarEnd,
			}, "\r\n"),
		},
		{
			Description:     "ActiveStateWithPlaceholderAndCursor",
			Placeholder:     "Placeholder",
			ValueWithCursor: " ",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd,
			}, "\r\n"),
		},
	}
	for _, tC := range testCases {
		t.Run(tC.Description, func(t *testing.T) {
			frame := theme.ApplyTheme(theme.Params[string]{
				Context: core.Prompt[string]{
					State: core.ActiveState,
				},
				Message:         "Test message",
				ValueWithCursor: tC.ValueWithCursor,
				Placeholder:     tC.Placeholder,
			})
			assert.Equal(t, tC.Expected, frame)
		})
	}
}

func TestApplyThemeErrorState(t *testing.T) {
	testCases := []struct {
		Description     string
		ValueWithCursor string
		Placeholder     string
		Error           string
		Expected        string
	}{
		{
			Description: "ErrorState",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " ",
			}, "\r\n"),
		},
		{
			Description: "ErrorStateWithPlaceholder",
			Placeholder: "Placeholder",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " Placeholder",
			}, "\r\n"),
		},
		{
			Description: "ErrorStateWithPlaceholderAndError",
			Placeholder: "Placeholder",
			Error:       "Error message",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " Placeholder",
				symbols.BarEnd + " Error message",
			}, "\r\n"),
		},
		{
			Description:     "ErrorStateWithValue",
			ValueWithCursor: "Value with cursor ",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " Value with cursor ",
			}, "\r\n"),
		},
		{
			Description:     "ErrorStateWithValueAndError",
			ValueWithCursor: "Value with cursor ",
			Error:           "Error message",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " Value with cursor ",
				symbols.BarEnd + " Error message",
			}, "\r\n"),
		},
		{
			Description:     "ErrorStateWithPlaceholderAndValue",
			Placeholder:     "Placeholder",
			ValueWithCursor: "Value with cursor ",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " Value with cursor ",
			}, "\r\n"),
		},
		{
			Description:     "ErrorStateWithWithPlaceholderAndValueAndError",
			Placeholder:     "Placeholder",
			ValueWithCursor: "Value with cursor ",
			Error:           "Error message",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepError + " Test message",
				symbols.Bar + " Value with cursor ",
				symbols.BarEnd + " Error message",
			}, "\r\n"),
		},
	}
	for _, tC := range testCases {
		t.Run(tC.Description, func(t *testing.T) {
			frame := theme.ApplyTheme(theme.Params[string]{
				Context: core.Prompt[string]{
					State: core.ErrorState,
					Error: tC.Error,
				},
				Message:         "Test message",
				ValueWithCursor: tC.ValueWithCursor,
				Placeholder:     tC.Placeholder,
			})
			assert.Equal(t, tC.Expected, frame)
		})
	}
}

func TestApplyThemeSubmitState(t *testing.T) {
	testCases := []struct {
		Description string
		Value       string
		Placeholder string
		Expected    string
	}{
		{
			Description: "SubmitState",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepSubmit + " Test message",
				symbols.Bar + " ",
			}, "\r\n"),
		},
		{
			Description: "SubmitStateWithPlaceholder",
			Placeholder: "Placeholder",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepSubmit + " Test message",
				symbols.Bar + " ",
			}, "\r\n"),
		},
		{
			Description: "SubmitStateWithValue",
			Value:       "Value",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepSubmit + " Test message",
				symbols.Bar + " Value",
			}, "\r\n"),
		},
		{
			Description: "SubmitStateWithPlaceholderAndValue",
			Placeholder: "Placeholder",
			Value:       "Value",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepSubmit + " Test message",
				symbols.Bar + " Value",
			}, "\r\n"),
		},
	}
	for _, tC := range testCases {
		t.Run(tC.Description, func(t *testing.T) {
			frame := theme.ApplyTheme(theme.Params[string]{
				Context: core.Prompt[string]{
					State: core.SubmitState,
				},
				Message:     "Test message",
				Value:       tC.Value,
				Placeholder: tC.Placeholder,
			})
			assert.Equal(t, tC.Expected, frame)
		})
	}
}

func TestApplyThemeCancelState(t *testing.T) {
	testCases := []struct {
		Description string
		Value       string
		Placeholder string
		Expected    string
	}{
		{
			Description: "CancelState",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepCancel + " Test message",
				symbols.Bar + " ",
			}, "\r\n"),
		},
		{
			Description: "CancelStateWithPlaceholder",
			Placeholder: "Placeholder",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepCancel + " Test message",
				symbols.Bar + " ",
			}, "\r\n"),
		},
		{
			Description: "CancelStateWithValue",
			Value:       "Value",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepCancel + " Test message",
				symbols.Bar + " Value",
				symbols.Bar,
			}, "\r\n"),
		},
		{
			Description: "CancelStateWithPlaceholderAndValue",
			Placeholder: "Placeholder",
			Value:       "Value",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepCancel + " Test message",
				symbols.Bar + " Value",
				symbols.Bar,
			}, "\r\n"),
		},
	}
	for _, tC := range testCases {
		t.Run(tC.Description, func(t *testing.T) {
			frame := theme.ApplyTheme(theme.Params[string]{
				Context: core.Prompt[string]{
					State: core.CancelState,
				},
				Message:     "Test message",
				Value:       tC.Value,
				Placeholder: tC.Placeholder,
			})
			assert.Equal(t, tC.Expected, frame)
		})
	}
}

func TestApplyThemeValidateState(t *testing.T) {
	testCases := []struct {
		Description        string
		Value              string
		Placeholder        string
		ValidationDuration time.Duration
		Expected           string
	}{
		{
			Description: "ValidateState",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating",
			}, "\r\n"),
		},
		{
			Description:        "ValidateStateAfterOneSecond",
			ValidationDuration: 1 * time.Second,
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating.",
			}, "\r\n"),
		},
		{
			Description:        "ValidateStateAfterTwoSecond",
			ValidationDuration: 2 * time.Second,
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating..",
			}, "\r\n"),
		},
		{
			Description:        "ValidateStateAfterThreeSecond",
			ValidationDuration: 3 * time.Second,
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating...",
			}, "\r\n"),
		},
		{
			Description:        "ValidateStateAfterFourSecond",
			ValidationDuration: 4 * time.Second,
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating",
			}, "\r\n"),
		},
		{
			Description:        "ValidateStateAfterFiveSecond",
			ValidationDuration: 5 * time.Second,
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating.",
			}, "\r\n"),
		},
		{
			Description: "ValidateStateWithPlaceholder",
			Placeholder: "Placeholder",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " ",
				symbols.BarEnd + " validating",
			}, "\r\n"),
		},
		{
			Description: "ValidateStateWithValue",
			Value:       "Value",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Value",
				symbols.BarEnd + " validating",
			}, "\r\n"),
		},
		{
			Description: "ValidateStateWithPlaceholderAndValue",
			Placeholder: "Placeholder",
			Value:       "Value",
			Expected: strings.Join([]string{
				symbols.Bar,
				symbols.StepActive + " Test message",
				symbols.Bar + " Value",
				symbols.BarEnd + " validating",
			}, "\r\n"),
		},
	}
	for _, tC := range testCases {
		t.Run(tC.Description, func(t *testing.T) {
			frame := theme.ApplyTheme(theme.Params[string]{
				Context: core.Prompt[string]{
					State:              core.ValidateState,
					IsValidating:       true,
					ValidationDuration: tC.ValidationDuration,
				},
				Message:     "Test message",
				Value:       tC.Value,
				Placeholder: tC.Placeholder,
			})
			assert.Equal(t, tC.Expected, frame)
		})
	}
}
