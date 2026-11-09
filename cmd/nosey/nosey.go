package main

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/ui"
	"github.com/spf13/cobra"
)

type commandError struct {
	code int
	err  error
}

func (e commandError) Error() string { return e.err.Error() }
func (e commandError) Unwrap() error { return e.err }
func exitCode(err error) int {
	var missing *exec.Error
	if errors.As(err, &missing) {
		return 6
	}
	var policy installation.PolicyError
	if errors.As(err, &policy) {
		return 2
	}
	var prerequisite installation.PrerequisiteError
	if errors.As(err, &prerequisite) {
		return 6
	}
	var typed commandError
	if errors.As(err, &typed) {
		return typed.code
	}
	return 2
}
func fail(code int, err error) error {
	if err == nil {
		return nil
	}
	return commandError{code, err}
}

type cliOptions struct {
	cwd, config string
	refresh     bool
	open        func(context.Context, string, string, bool) (app.Session, error)
}

func (o *cliOptions) store() (settings.Store, error) {
	cwd := o.cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return settings.Store{}, err
		}
	}
	return settings.New(cwd, o.config)
}
func (o *cliOptions) session(ctx context.Context, refresh bool) (app.Session, error) {
	store, err := o.store()
	if err != nil {
		return app.Session{}, err
	}
	if _, resolveErr := store.Resolve(); resolveErr != nil {
		return app.Session{}, resolveErr
	}
	open := o.open
	if open == nil {
		open = app.Open
	}
	s, err := open(ctx, store.Workspace, o.config, refresh || o.refresh)
	if err != nil {
		return s, fail(3, err)
	}
	return s, nil
}
func (o *cliOptions) configuredSession(ctx context.Context) (app.Session, error) {
	if o.open != nil {
		return o.session(ctx, false)
	}
	store, err := o.store()
	if err != nil {
		return app.Session{}, err
	}
	return app.Configure(store.Workspace, o.config)
}
func emit[T any](cmd *cobra.Command, schema string, value T) error {
	data, err := contracts.Encode(schema, value)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}
func newRootCmd() *cobra.Command { return newCommandTree(nil) }
func newCommandTree(open func(context.Context, string, string, bool) (app.Session, error)) *cobra.Command {
	o := &cliOptions{open: open}
	showVersion := false
	root := &cobra.Command{Use: "nosey", Short: "Discover and install agent skills for your workspace", Args: cobra.NoArgs, SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().StringVar(&o.cwd, "cwd", "", "workspace root (defaults to current directory; never ascends)")
	root.PersistentFlags().StringVarP(&o.config, "config", "c", "", "user JSON settings file")
	root.PersistentFlags().BoolVar(&o.refresh, "refresh", false, "refresh registry snapshots now, even within the refresh interval (TTL)")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print version")
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if showVersion {
			cmd.Println(resolvedVersion())
			return nil
		}
		return runNoseyUI(cmd, o)
	}
	root.AddCommand(&cobra.Command{Use: "ui", Short: "Open the four-tab workspace interface", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return runNoseyUI(cmd, o) }})
	root.AddCommand(scanCommand(o), searchCommand(o), installCommand(o), updateCommand(o), uninstallCommand(o), registryCommand(o), settingsCommand(o), trustCommand(o), listCommand(o), doctorCommand(o), newCompletionCmd(), newSkillsCmd(root))
	return root
}

func confirm(cmd *cobra.Command, message string) (bool, error) {
	return prompts.Confirm(prompts.ConfirmParams{Context: cmd.Context(), Message: message, InitialValue: false, Input: os.Stdin, Output: os.Stderr})
}
func runNoseyUI(cmd *cobra.Command, o *cliOptions) error {
	if err := requireInteractive(os.Stdin, os.Stdout); err != nil {
		return err
	}
	store, err := o.store()
	if err != nil {
		return err
	}
	resolved, err := store.Resolve()
	if err != nil {
		return err
	}
	return ui.RunNosey(cmd.Context(), store.Workspace, o.config, resolved.Settings.UI.Mode, o.refresh)
}
