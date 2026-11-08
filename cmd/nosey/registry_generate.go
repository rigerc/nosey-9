package main

import (
	"slices"

	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/spf13/cobra"
)

func generateCommand() *cobra.Command {
	check, jsonMode := false, false
	cmd := &cobra.Command{
		Use:   "generate PATH",
		Short: "Canonicalize registry data and write the manifest and lock",
		Long: "Canonicalize registry data and write the manifest and lock.\n\n" +
			"Generation is offline. Use --check to verify that data files, the manifest,\n" +
			"and the lock are current without writing anything.",
		Example: "  nosey registry generate ./my-registry\n  nosey registry generate ./my-registry --check",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := registrygen.Run(args[0], registrygen.Options{Check: check, Command: "generate"})
			if err != nil {
				return fail(registrygen.Code(err), err)
			}
			if jsonMode {
				return emit(cmd, "registry-generation-output.schema.json", result)
			}
			if len(result.ChangedFiles) == 0 {
				cmd.Println("Registry is up to date")
				return nil
			}
			for _, file := range result.ChangedFiles {
				cmd.Printf("wrote %s\n", file)
			}
			for _, file := range result.RemovedFiles {
				cmd.Printf("removed %s\n", file)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report drift without writing (exit 2)")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit registry-generation JSON v1")
	return cmd
}

func refreshCommand() *cobra.Command {
	jsonMode := false
	cmd := &cobra.Command{
		Use:   "refresh PATH",
		Short: "Fetch declared upstream data files, then regenerate",
		Long: "Fetch every data file declared under `upstreams`, replace it only after\n" +
			"all fetches succeed, then canonicalize and rewrite the manifest and lock.\n\n" +
			"Registries without upstreams are left unchanged. This is the only registry\n" +
			"command that uses the network.",
		Example: "  nosey registry refresh ./my-registry",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := registrygen.Refresh(cmd.Context(), args[0], nil)
			if err != nil {
				return fail(registrygen.Code(err), err)
			}
			if jsonMode {
				return emit(cmd, "registry-generation-output.schema.json", result)
			}
			if len(result.ChangedFiles) == 0 {
				cmd.Println("Registry is up to date")
				return nil
			}
			slices.Sort(result.ChangedFiles)
			for _, file := range result.ChangedFiles {
				cmd.Printf("wrote %s\n", file)
			}
			for _, file := range result.RemovedFiles {
				cmd.Printf("removed %s\n", file)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit registry-generation JSON v1")
	return cmd
}
