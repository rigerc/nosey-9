package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func trustCommand(o *cliOptions) *cobra.Command {
	revoke, yes := false, false
	cmd := &cobra.Command{Use: "trust", Short: "Explicitly approve or revoke workspace registry fingerprints", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "revoke current workspace registry approval")
	cmd.Flags().BoolVar(&yes, "yes", false, "approve the displayed current fingerprint noninteractively")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		if revoke {
			return store.Trust("", true)
		}
		cmd.Printf("Workspace: %s\nFingerprint: %s\n", store.Workspace, resolved.Fingerprint)
		for _, ref := range resolved.Pending {
			cmd.Printf("%s %s%s\n", ref.ID, ref.Source.URI, ref.Source.Path)
		}
		if !yes {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("explicit --yes or interactive approval required")
			}
			yes, err = confirm(cmd, "Approve this workspace registry fingerprint?")
			if err != nil {
				return err
			}
		}
		if !yes {
			return nil
		}
		return store.Trust(resolved.Fingerprint, false)
	}
	return cmd
}
