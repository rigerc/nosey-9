package main

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/go-cli-template/internal/greeting"
	"github.com/go-cli-template/internal/logging"
	"github.com/go-cli-template/internal/runtimeconfig"
)

// newGreetCmd demonstrates the agentic template's testable application
// boundary without changing the CLI's existing TOML settings or TUI.
func newGreetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "greet [name]",
		Short: "Emit a structured greeting (example application)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "world"
			if len(args) != 0 {
				name = args[0]
			}
			return runGreet(cmd.OutOrStdout(), os.LookupEnv, name)
		},
	}
}

// runGreet injects its writer and environment lookup for deterministic tests.
func runGreet(out io.Writer, lookup runtimeconfig.LookupFunc, name string) error {
	cfg, err := runtimeconfig.Load(lookup)
	if err != nil {
		return err
	}

	logger := logging.New(out, cfg)
	greeting.Run(logger, cfg, name)
	return nil
}
