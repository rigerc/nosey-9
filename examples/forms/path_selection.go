package main

import (
	"strings"

	"github.com/go-cli-template/internal/clack/prompts"
)

func runSelectPath() error {
	value, err := prompts.SelectPath(prompts.SelectPathParams{
		Message:      "Browse and select a file",
		InitialValue: ".",
		Filter:       true,
	})
	if err != nil {
		return err
	}
	show("Select-path", value)
	return nil
}

func runMultiSelectPath() error {
	values, err := prompts.MultiSelectPath(prompts.MultiSelectPathParams{
		Message:     "Select one or more files (space toggles)",
		InitialPath: ".",
		Required:    true,
		Filter:      true,
	})
	if err != nil {
		return err
	}
	show("Multi-select-path", strings.Join(values, ", "))
	return nil
}
