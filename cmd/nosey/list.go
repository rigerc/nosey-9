package main

import "github.com/spf13/cobra"

func listCommand(o *cliOptions) *cobra.Command {
	jsonMode := false
	cmd := &cobra.Command{Use: "list", Short: "Read project-scoped installed skill inventory", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit project inventory JSON v1")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		result := s.Installer.Inventory(cmd.Context(), s.Catalog)
		if jsonMode {
			return emit(cmd, "list-output.schema.json", result)
		}
		for _, item := range result.Skills {
			cmd.Printf("%s  %s\n", item.Name, item.Status)
		}
		return nil
	}
	return cmd
}
