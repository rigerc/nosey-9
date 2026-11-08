package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/rigerc/nosey-9/internal/clack/core"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
)

// newPromptCmd exposes the migrated go-clack prompt runtime through a small
// workflow example. All steps share one native Bubble Tea program.
func newPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Run a go-clack prompt workflow (example)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrompt(cmd)
		},
	}
}

func runPrompt(cmd *cobra.Command) error {
	if err := requireInteractive(os.Stdin, os.Stdout); err != nil {
		return err
	}

	var result struct {
		Name    string
		Confirm bool
	}

	err := prompts.Workflow(&result).
		ModelStep("Name", func() core.ComposablePrompt {
			return prompts.TextModel(prompts.TextParams{
				Message:     "What should we call you?",
				Placeholder: "Ada",
				Required:    true,
			})
		}).
		ModelStep("Confirm", func() core.ComposablePrompt {
			return prompts.ConfirmModel(prompts.ConfirmParams{
				Message:      "Create the greeting?",
				InitialValue: true,
			})
		}).
		RunNative()
	if err != nil {
		return err
	}

	if !result.Confirm {
		cmd.Println("Cancelled.")
		return nil
	}
	cmd.Printf("Hello, %s!\n", result.Name)
	return nil
}
