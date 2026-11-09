package main

import (
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/spf13/cobra"
)

func registryCICommand(o *cliOptions) *cobra.Command {
	opts := registry.CIUpdateOptions{}
	update := &cobra.Command{
		Use:   "update PATH",
		Short: "Update a scaffolded registry's CI files from its template",
		Long: "Replace a registry's CI workflow and Dependabot files with the current\n" +
			"versions from the template provider recorded by `nosey registry init`,\n" +
			"and add them if the registry was created without --ci.\n\n" +
			"Files edited since they were generated are reported and nothing is written,\n" +
			"unless --force is given. Registries from older versions of nosey have no\n" +
			"record of their CI files, so a differing file needs --force once.",
		Example: "  nosey registry ci update ./my-registry\n" +
			"  nosey registry ci update ./my-registry --force",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := o.session(cmd.Context(), false)
			if err != nil {
				return err
			}
			opts.Path = args[0]
			result, err := s.Registry.UpdateCI(s.Catalog, opts)
			if err != nil {
				return err
			}
			printCIUpdate(cmd, result)
			return nil
		},
	}
	update.Flags().StringVar(&opts.RegistryID, "template-registry", "", "configured template provider ID (the one used at init by default)")
	update.Flags().BoolVar(&opts.Force, "force", false, "replace CI files that were edited locally")
	root := &cobra.Command{Use: "ci", Short: "Manage a registry's generated CI files"}
	root.AddCommand(update)
	return root
}

func printCIUpdate(cmd *cobra.Command, result registry.CIUpdateResult) {
	for _, entry := range []struct {
		verb  string
		files []string
	}{{"added", result.Added}, {"updated", result.Updated}, {"removed", result.Removed}, {"kept (edited, no longer in the template)", result.Kept}} {
		for _, file := range entry.files {
			cmd.Printf("%s %s\n", entry.verb, file)
		}
	}
	if len(result.Added)+len(result.Updated)+len(result.Removed) == 0 {
		cmd.Println("CI files are up to date")
	}
	if result.GeneratorFrom != "" && result.GeneratorFrom != result.GeneratorTo {
		cmd.Printf("generator %.7s -> %.7s\n", result.GeneratorFrom, result.GeneratorTo)
	}
}
