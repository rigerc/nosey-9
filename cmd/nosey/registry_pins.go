package main

import (
	"fmt"

	"github.com/rigerc/nosey-9/internal/registrypins"
	"github.com/rigerc/nosey-9/internal/storage"
	"github.com/spf13/cobra"
)

const (
	quarantineReport  = "report"
	quarantineEnforce = "enforce"
)

func registryPinsCommand() *cobra.Command {
	root := &cobra.Command{Use: "pins", Short: "Check and update a custom registry's commit-pinned skills"}
	root.AddCommand(pinsCheckCommand(), pinsUpdateCommand())
	return root
}

func pinsCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "check PATH",
		Short:   "Report pinned skills and check quarantine history without using the network",
		Example: "  nosey registry pins check ./my-registry",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := registrypins.Check(args[0])
			if err != nil {
				return fail(1, err)
			}
			cmd.Printf("%d pinned, %d not commit-pinned, %d quarantined, %d with open misses\n", result.Pinned, result.Unpinned, result.Quarantined, result.Open)
			for _, problem := range result.Problems {
				cmd.PrintErrln(problem)
			}
			if len(result.Problems) > 0 {
				return fail(1, fmt.Errorf("pin state has %d problems", len(result.Problems)))
			}
			return nil
		},
	}
}

func pinsUpdateCommand() *cobra.Command {
	quarantine, summaryFile, timeout := quarantineReport, "", registrypins.DefaultTimeout
	cmd := &cobra.Command{
		Use:   "update PATH",
		Short: "Re-pin skills that changed upstream and report skills that vanished",
		Long: "Compare every skill pinned to a full commit with its repository's default\n" +
			"branch, then regenerate the registry. A skill is re-pinned only when its\n" +
			"directory differs from the pinned commit's.\n\n" +
			"A skill that is gone is recorded as a miss in nosey.pins.json. After two\n" +
			"misses at least five days apart it is reported, or quarantined with\n" +
			"--quarantine enforce. Network errors, rate limits and repositories that\n" +
			"were never readable, such as private ones, never count as misses.\n\n" +
			"Only skills whose source ref is a full commit are affected.",
		Example: "  nosey registry pins update ./my-registry\n" +
			"  nosey registry pins update ./my-registry --quarantine enforce --summary-file summary.md",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if quarantine != quarantineReport && quarantine != quarantineEnforce {
				return fail(1, fmt.Errorf("unsupported --quarantine value %q (expected report or enforce)", quarantine))
			}
			result, err := registrypins.Update(cmd.Context(), args[0], registrypins.Options{Enforce: quarantine == quarantineEnforce, Timeout: timeout})
			if err != nil {
				return fail(1, err)
			}
			text := result.Summary.Markdown()
			cmd.Print(text)
			if summaryFile != "" {
				if err = storage.Write(summaryFile, []byte(text)); err != nil {
					return fail(1, err)
				}
			}
			for _, file := range result.Changed {
				cmd.Printf("wrote %s\n", file)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&quarantine, "quarantine", quarantine, "what to do with skills that stay missing: report or enforce")
	cmd.Flags().StringVar(&summaryFile, "summary-file", "", "also write the Markdown summary to this file")
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "stop probing after this long; the rest wait for the next run")
	return cmd
}
