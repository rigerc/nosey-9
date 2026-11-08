package catalog

import (
	"fmt"
	"github.com/rigerc/nosey-9/internal/domain"
	"math"
	"slices"
)

// ValidateFilters rejects unknown facets without changing detection or source authorization.
func ValidateFilters(c domain.Catalog, f domain.Filters) error {
	if !slices.Contains([]string{"technologies", "skills", "both"}, f.QueryTarget) {
		return fmt.Errorf("invalid query target")
	}
	if !slices.Contains([]string{"any", "scored", "unscored"}, f.ScoreState) {
		return fmt.Errorf("invalid score state")
	}
	for _, group := range []struct {
		values, allowed []string
		name            string
	}{{f.Statuses, []string{"any", "installed", "missing", "unknown"}, "status"}, {f.Qualities, []string{"excellent", "good", "fair", "poor", "unassessed"}, "quality"}, {f.Priorities, []string{"core", "recommended", "optional"}, "priority"}, {f.RuleKinds, []string{"file", "dependency", "content", "extension", "dotenv", "detector", "mcp_package"}, "rule kind"}} {
		for _, value := range group.values {
			if !slices.Contains(group.allowed, value) {
				return fmt.Errorf("invalid %s %s", group.name, value)
			}
		}
	}
	for _, id := range append(slices.Clone(f.Technologies), f.RecommendationTechnologies...) {
		if _, ok := c.Technologies[id]; !ok {
			return fmt.Errorf("unknown technology filter %s", id)
		}
	}
	for _, id := range f.Registries {
		if _, ok := c.Roots[id]; !ok {
			return fmt.Errorf("unknown registry filter %s", id)
		}
	}
	for _, id := range f.Repositories {
		if _, ok := c.Repositories[id]; ok {
			continue
		}
		found := false
		for _, s := range c.Skills {
			if s.RepositoryID == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown repository filter %s", id)
		}
	}
	for _, value := range []*float64{f.MinScore, f.MinRepositoryAverage} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 100) {
			return fmt.Errorf("score threshold must be 0..100")
		}
	}
	return nil
}

// ValidateSort checks the stable comparator names used by CLI and TUI.
func ValidateSort(order string, technologies, search bool) error {
	allowed := []string{"priority-desc", "score-desc", "quality-desc", "repository-average-desc", "name-asc"}
	if technologies {
		allowed = []string{"name-asc", "name-desc", "evidence-count-desc", "matched-rules-desc"}
	}
	if search {
		allowed = append(allowed, "relevance")
	}
	if !slices.Contains(allowed, order) {
		return fmt.Errorf("unsupported sort %s", order)
	}
	return nil
}
