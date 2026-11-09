package main

import (
	"fmt"
	"slices"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/spf13/cobra"
)

func filterFlags(cmd *cobra.Command, f *domain.Filters, scan bool) {
	flags := cmd.Flags()
	flags.StringVar(&f.Query, "query", "", "case-insensitive query")
	flags.StringSliceVar(&f.Technologies, "technology", nil, "technology IDs")
	flags.StringSliceVar(&f.Registries, "registry", nil, "source registry IDs (result filter)")
	flags.StringSliceVar(&f.Repositories, "repository", nil, "source repository IDs")
	flags.StringSliceVar(&f.Statuses, "status", nil, "any, installed, missing, outdated or unknown")
	flags.StringSliceVar(&f.Qualities, "quality", nil, "excellent, good, fair, poor or unassessed")
	flags.StringVar(&f.ScoreState, "score-state", "any", "any, scored or unscored")
	flags.Float64("min-score", 0, "minimum authored score (0..100)")
	flags.Float64("min-repo-average", 0, "minimum authored repository average (0..100)")
	if scan {
		flags.StringSliceVar(&f.RecommendationTechnologies, "rec-technology", nil, "recommendation technology IDs")
		flags.StringSliceVar(&f.RuleKinds, "rule-kind", nil, "matching evidence rule kinds")
		flags.StringSliceVar(&f.Priorities, "priority", nil, "core, recommended or optional")
		flags.StringVar(&f.QueryTarget, "query-target", "both", "technologies, skills or both")
	}
}
func normalizeFilters(cmd *cobra.Command, f *domain.Filters, c domain.Catalog) error {
	for _, facet := range []*[]string{&f.Technologies, &f.RecommendationTechnologies, &f.RuleKinds, &f.Registries, &f.Repositories, &f.Priorities, &f.Statuses, &f.Qualities} {
		if *facet == nil {
			*facet = []string{}
		}
	}
	if f.QueryTarget == "" {
		f.QueryTarget = "both"
	}
	if !slices.Contains([]string{"both", "technologies", "skills"}, f.QueryTarget) {
		return fmt.Errorf("invalid query-target")
	}
	if !slices.Contains([]string{"any", "scored", "unscored"}, f.ScoreState) {
		return fmt.Errorf("invalid score-state")
	}
	for _, status := range f.Statuses {
		if !slices.Contains([]string{"any", "installed", "missing", "outdated", "unknown"}, status) {
			return fmt.Errorf("invalid status %s", status)
		}
	}
	if slices.Contains(f.Statuses, "any") {
		f.Statuses = []string{}
	}
	for _, id := range f.Registries {
		if _, ok := c.Roots[id]; !ok {
			return fmt.Errorf("unknown registry filter %s", id)
		}
	}
	for _, item := range []struct {
		name   string
		target **float64
	}{{"min-score", &f.MinScore}, {"min-repo-average", &f.MinRepositoryAverage}} {
		if cmd.Flags().Changed(item.name) {
			v, _ := cmd.Flags().GetFloat64(item.name)
			if v < 0 || v > 100 {
				return fmt.Errorf("%s must be 0..100", item.name)
			}
			*item.target = &v
		}
	}
	return nil
}
