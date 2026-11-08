package registry

import (
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
	"slices"
)

func semanticSkills(c *domain.Catalog) {
	invalid := map[string]string{}
	for id, s := range c.Skills {
		if s.RepositoryID != "" {
			if _, ok := c.Repositories[s.RepositoryID]; !ok {
				invalid[id] = "Unknown repository " + s.RepositoryID
			}
		}
		for _, t := range s.Technologies {
			if _, ok := c.Technologies[t]; !ok {
				invalid[id] = "Unknown technology association " + t
			}
		}
		for _, required := range s.Requires {
			if _, ok := c.Skills[required]; !ok || required == id {
				invalid[id] = "Unknown or self-linked requirement " + required
			}
			if slices.Contains(s.ConflictsWith, required) {
				invalid[id] = "Contradictory requirement/conflict " + required
			}
		}
		for _, other := range s.ConflictsWith {
			target, ok := c.Skills[other]
			if !ok || other == id || !slices.Contains(target.ConflictsWith, id) {
				invalid[id] = "Unknown, self-linked or asymmetric conflict " + other
			}
		}
		if s.Source.Kind == "local" {
			if _, err := storage.Read(c.Roots[s.RegistryID], s.Source.Path+"/SKILL.md", 1<<20); err != nil {
				invalid[id] = "Local skill payload is unavailable or escapes its registry"
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for id, s := range c.Skills {
			if invalid[id] != "" {
				continue
			}
			for _, required := range s.Requires {
				if invalid[required] != "" {
					invalid[id] = "Invalid prerequisite " + required
					changed = true
					break
				}
			}
		}
	}
	for id, reason := range invalid {
		delete(c.Skills, id)
		c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "E_SKILL_REFERENCE", Severity: "error", Message: reason, Subject: id})
	}
	for id := range c.Scores {
		if _, ok := c.Repositories[id]; !ok {
			delete(c.Scores, id)
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_REPOSITORY_SCORE", Severity: "warning", Message: "Ignoring score for an undeclared repository", Subject: id})
		}
	}
}
