package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rigerc/nosey-9/internal/studio"
	"github.com/spf13/cobra"
)

func studioCommand(o *cliOptions) *cobra.Command {
	port, noOpen, checks, local := 0, false, false, false
	cmd := &cobra.Command{
		Use:   "studio PATH",
		Short: "Edit registry skills, mappings and technology rules in a local web UI",
		Long: "Serve a local web editor for a registry checkout on 127.0.0.1.\n\n" +
			"The studio shows the latest remote default branch without moving your\n" +
			"checkout. It falls back to the checkout, with a warning, when the remote\n" +
			"is unavailable. Use --local to edit the checkout as it stands.\n\n" +
			"Edits stay in a draft outside the registry until you apply them to the\n" +
			"working tree or publish them. Publishing replays the draft on the remote\n" +
			"default branch in a temporary worktree and opens a pull request with gh.\n\n" +
			"--registry-checks also runs the registry's own Node scripts before writing.\n" +
			"They execute code from that checkout, so only use it on a registry you\n" +
			"trust. On the official registry it enables skill deletion, repository\n" +
			"changes and manual assessments, which need those scripts to stay valid.",
		Example: "  nosey registry studio registries/default\n" +
			"  nosey registry studio ./my-registry --port 4173 --no-open\n" +
			"  nosey registry studio registries/default --registry-checks\n" +
			"  nosey registry studio registries/default --local",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := o.store()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(contextOf(cmd), os.Interrupt, syscall.SIGTERM)
			defer stop()
			options := studio.Options{Root: args[0], Store: store, RegistryChecks: checks, Local: local}
			return fail(3, studio.Run(ctx, options, port, !noOpen, cmd.OutOrStdout()))
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "loopback port to listen on (default: any free port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the URL without opening a browser")
	cmd.Flags().BoolVar(&checks, "registry-checks", false, "run the registry's own Node scripts before writing")
	cmd.Flags().BoolVar(&local, "local", false, "edit the local checkout instead of the remote default branch")
	return cmd
}

func contextOf(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
