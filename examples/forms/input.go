package main

import (
	"errors"
	"fmt"

	"github.com/go-cli-template/internal/clack/prompts"
)

var (
	errShortName     = errors.New("enter at least two characters")
	errShortPassword = errors.New("use at least four characters")
)

func runText() error {
	value, err := prompts.Text(prompts.TextParams{
		Message:     "What should we call you?",
		Placeholder: "Ada Lovelace",
		Required:    true,
		Validate: func(value string) error {
			if len(value) < 2 {
				return errShortName
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	show("Text", value)
	return nil
}

func runPassword() error {
	value, err := prompts.Password(prompts.PasswordParams{
		Message:  "Choose a demo password",
		Required: true,
		Validate: func(value string) error {
			if len(value) < 4 {
				return errShortPassword
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	show("Password", fmt.Sprintf("accepted, %d characters (value hidden)", len(value)))
	return nil
}

func runConfirm() error {
	value, err := prompts.Confirm(prompts.ConfirmParams{
		Message:      "Keep going with the gallery?",
		InitialValue: true,
		Active:       "yes, continue",
		Inactive:     "no, stop here",
	})
	if err != nil {
		return err
	}
	show("Confirm", fmt.Sprintf("%t", value))
	return nil
}

func runPath() error {
	value, err := prompts.Path(prompts.PathParams{
		Message:      "Pick a directory (tab completes)",
		InitialValue: ".",
		OnlyShowDir:  true,
		Required:     true,
	})
	if err != nil {
		return err
	}
	show("Path", value)
	return nil
}
