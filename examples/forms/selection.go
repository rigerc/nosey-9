package main

import (
	"strings"

	"github.com/rigerc/nosey-9/internal/clack/prompts"
)

func runSelect() error {
	value, err := prompts.Select(prompts.SelectParams[string]{
		Message:      "Choose a language",
		InitialValue: "go",
		Filter:       true,
		Options: []*prompts.SelectOption[string]{
			{Label: "Go", Value: "go", Hint: "fast builds and a small binary"},
			{Label: "Rust", Value: "rust", Hint: "zero-cost abstractions"},
			{Label: "TypeScript", Value: "typescript", Hint: "one language everywhere"},
			{Label: "Python", Value: "python", Hint: "batteries included"},
		},
	})
	if err != nil {
		return err
	}
	show("Select", value)
	return nil
}

func runMultiSelect() error {
	values, err := prompts.MultiSelect(prompts.MultiSelectParams[string]{
		Message:      "Choose project tooling",
		InitialValue: []string{"lint"},
		Filter:       true,
		Options: []*prompts.MultiSelectOption[string]{
			{Label: "Lint", Value: "lint", Hint: "golangci-lint and anti-slop", IsSelected: true},
			{Label: "Tests", Value: "tests", Hint: "race detector enabled"},
			{Label: "Examples", Value: "examples", Hint: "Runnable CLI examples"},
			{Label: "Release", Value: "release", Hint: "Homebrew and AUR recipes"},
		},
	})
	if err != nil {
		return err
	}
	show("Multi-select", strings.Join(values, ", "))
	return nil
}

func runGroupMultiSelect() error {
	values, err := prompts.GroupMultiSelect(prompts.GroupMultiSelectParams[string]{
		Message:      "Choose features by group",
		InitialValue: []string{"api"},
		SpacedGroups: true,
		Options: map[string][]prompts.MultiSelectOption[string]{
			"Core": {
				{Label: "Config", Value: "config", Hint: "TOML settings", IsSelected: true},
				{Label: "Logging", Value: "logging", Hint: "slog handlers"},
			},
			"Surfaces": {
				{Label: "API", Value: "api", Hint: "HTTP handlers", IsSelected: true},
				{Label: "CLI", Value: "cli", Hint: "Cobra commands"},
			},
		},
	})
	if err != nil {
		return err
	}
	show("Group multi-select", strings.Join(values, ", "))
	return nil
}

func runSelectKey() error {
	value, err := prompts.SelectKey(prompts.SelectKeyParams[string]{
		Message: "Press a key to pick a color",
		Options: []prompts.SelectKeyOption[string]{
			{Label: "green", Value: "green", Key: "g"},
			{Label: "blue", Value: "blue", Key: "b"},
			{Label: "magenta", Value: "magenta", Key: "m"},
		},
	})
	if err != nil {
		return err
	}
	show("Select-key", value)
	return nil
}

func runReorder() error {
	values, err := prompts.Reorder(prompts.ReorderParams[string]{
		Message: "Reorder the steps (space picks up, arrows move)",
		Options: []*prompts.ReorderOption[string]{
			{Label: "Plan", Value: "plan"},
			{Label: "Build", Value: "build"},
			{Label: "Test", Value: "test"},
			{Label: "Ship", Value: "ship"},
		},
	})
	if err != nil {
		return err
	}
	show("Reorder", strings.Join(values, " -> "))
	return nil
}
