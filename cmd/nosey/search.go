package main

import (
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/spf13/cobra"
)

func searchCommand(o *cliOptions) *cobra.Command {
	f := domain.DefaultFilters()
	order := "relevance"
	jsonMode := false
	cmd := &cobra.Command{Use: "search [QUERY]", Short: "Browse the effective skill catalog", Args: cobra.MaximumNArgs(1)}
	filterFlags(cmd, &f, false)
	cmd.Flags().StringVar(&order, "sort", "relevance", "relevance, score-desc, quality-desc, repository-average-desc, name-asc")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit search JSON v2")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		if len(args) > 0 {
			f.Query = args[0]
		}
		if err = normalizeFilters(cmd, &f, s.Catalog); err != nil {
			return err
		}
		if err = catalog.ValidateFilters(s.Catalog, f); err != nil {
			return err
		}
		if err = catalog.ValidateSort(order, false, true); err != nil {
			return err
		}
		_ = s.Inventory(cmd.Context())
		result := catalog.Search(s.Catalog, f, order)
		if jsonMode {
			return emit(cmd, "search-output.schema.json", result)
		}
		for _, g := range result.Groups {
			cmd.Println(g.Name)
			for _, r := range g.Repositories {
				cmd.Printf("  %s\n", r.Name)
				for _, skill := range r.Skills {
					cmd.Printf("    %s  %s\n", skill.ID, skill.Name)
				}
			}
		}
		return nil
	}
	return cmd
}
