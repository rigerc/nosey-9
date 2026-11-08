package main

import (
	"fmt"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/spf13/cobra"
)

func scanCommand(o *cliOptions) *cobra.Command {
	f := domain.DefaultFilters()
	sort := domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}
	jsonMode, strict, mcp := false, false, false
	cmd := &cobra.Command{Use: "scan", Short: "Detect technologies and recommend skills", Args: cobra.NoArgs}
	filterFlags(cmd, &f, true)
	cmd.Flags().StringVar(&sort.Technologies, "sort-technologies", "name-asc", "name-asc, name-desc, evidence-count-desc, matched-rules-desc")
	cmd.Flags().StringVar(&sort.Skills, "sort-skills", "priority-desc", "priority-desc, score-desc, quality-desc, repository-average-desc, name-asc")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit scan JSON v2")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 4 when required evaluation is incomplete")
	cmd.Flags().BoolVar(&mcp, "enable-mcp", false, "passively inspect project MCP package references")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		if err = normalizeFilters(cmd, &f, s.Catalog); err != nil {
			return err
		}
		if err = catalog.ValidateFilters(s.Catalog, f); err != nil {
			return err
		}
		if err = catalog.ValidateSort(sort.Technologies, true, false); err != nil {
			return err
		}
		if err = catalog.ValidateSort(sort.Skills, false, false); err != nil {
			return err
		}
		_ = s.Inventory(cmd.Context())
		result, _, err := s.Scan(cmd.Context(), f, sort, mcp)
		if err != nil {
			return fail(4, err)
		}
		if jsonMode {
			err = emit(cmd, "scan-output.schema.json", result)
		} else {
			for _, t := range result.Technologies {
				cmd.Printf("%s (%d evidence items)\n", t.ID, len(t.Evidence))
			}
			for _, skill := range result.Skills {
				cmd.Printf("%s  %s  %s\n", skill.ID, skill.Priority, strings.Join(skill.Reasons, "; "))
			}
		}
		if err != nil {
			return err
		}
		if !result.Complete {
			cmd.PrintErrln("Warning: required rule evaluation is incomplete; inspect coverage gaps.")
			if strict {
				return fail(4, fmt.Errorf("strict scan incomplete"))
			}
		}
		return nil
	}
	return cmd
}
