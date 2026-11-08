package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

// Load merges enabled sources while retaining field provenance and invalidation.
func (s Service) Load(ctx context.Context, resolved settings.Resolved, refresh bool) (domain.Catalog, error) {
	c := domain.Catalog{Raw: map[string]map[string]json.RawMessage{}, History: map[string]map[string][]string{}, Sources: map[string]domain.RegistrySource{}, ProjectSources: resolved.WorkspaceIDs, Technologies: map[string]domain.Technology{}, Skills: map[string]domain.Skill{}, Mappings: map[string]domain.Mapping{}, Repositories: map[string]domain.Repository{}, Scores: map[string]domain.RepositoryScore{}, Provenance: map[string]map[string]string{}, Roots: map[string]string{}, Revisions: map[string]*string{}, InvalidTechnologies: map[string]string{}, Diagnostics: append([]domain.Diagnostic{}, resolved.Diagnostics...)}
	refs := slices.Clone(resolved.Settings.Registries)
	slices.SortStableFunc(refs, func(a, b domain.RegistryRef) int {
		if a.Priority != b.Priority {
			return a.Priority - b.Priority
		}
		if a.ID == "nosey:official" {
			return -1
		}
		if b.ID == "nosey:official" {
			return 1
		}
		return 0
	})
	collections := map[string]map[string]domain.RawEntity{"technologies": {}, "skills": {}, "mappings": {}, "repositories": {}}
	invalid := map[string]string{}
	for _, ref := range refs {
		if !ref.Enabled {
			continue
		}
		snap, err := s.Resolve(ctx, ref, resolved.Settings.Updates.TTL, refresh)
		if err != nil {
			if ref.ID == "nosey:official" {
				return c, err
			}
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_REGISTRY_UNAVAILABLE", Severity: "warning", Message: storage.Safe(err.Error()), Subject: ref.ID})
			continue
		}
		if err = validateSource(snap.Root); err != nil {
			return c, err
		}
		c.Roots[ref.ID] = snap.Root
		c.Sources[ref.ID] = ref.Source
		c.Revisions[ref.ID] = snap.Revision
		if snap.Stale {
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_STALE_CACHE", Severity: "warning", Message: "Using last validated snapshot", Subject: ref.ID})
		}
		for id, score := range snap.Manifest.Scores {
			countProvided := score.Count != nil
			if previous, ok := c.Scores[id]; ok && score.Count == nil {
				score.Count = previous.Count
			}
			c.Scores[id] = score
			key := "repositoryScores:" + id
			if c.Provenance[key] == nil {
				c.Provenance[key] = map[string]string{}
			}
			c.Provenance[key]["averageScore"] = ref.ID
			if countProvided {
				c.Provenance[key]["scoreCount"] = ref.ID
			}
		}
		for kind, name := range snap.Manifest.Files {
			data, readErr := storage.Read(snap.Root, name, 16<<20)
			if readErr != nil {
				return c, readErr
			}
			var doc map[string]json.RawMessage
			if err = json.Unmarshal(data, &doc); err != nil {
				return c, err
			}
			var entities []domain.RawEntity
			if err = json.Unmarshal(doc[kind], &entities); err != nil {
				return c, err
			}
			seen := map[string]bool{}
			if kind == "skills" {
				sums := map[string]float64{}
				counts := map[string]int{}
				for _, entity := range entities {
					var repo string
					var score *float64
					var disabled bool
					_ = json.Unmarshal(entity["repositoryId"], &repo)
					_ = json.Unmarshal(entity["score"], &score)
					_ = json.Unmarshal(entity["disabled"], &disabled)
					if repo != "" && score != nil && !disabled {
						sums[repo] += *score
						counts[repo]++
					}
				}
				for id, score := range snap.Manifest.Scores {
					if counts[id] > 0 && math.Abs(score.Average-sums[id]/float64(counts[id])) > 0.5 {
						c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_SCORE_DRIFT", Severity: "warning", Message: "Authored repository average differs from this source's scored-skill mean", Subject: ref.ID + ":" + id})
					}
				}
			}
			for _, entity := range entities {
				var id string
				if err = json.Unmarshal(entity["id"], &id); err != nil {
					return c, err
				}
				if seen[id] {
					return c, fmt.Errorf("duplicate %s ID %s", kind, id)
				}
				seen[id] = true
				key := kind + ":" + id
				if c.Provenance[key] == nil {
					c.Provenance[key] = map[string]string{}
				}
				base := collections[kind][id]
				if base == nil {
					base = domain.RawEntity{}
				}
				if err = mergeEntity(base, entity, kind, c.Provenance[key], ref.ID); err != nil {
					invalid[key] = err.Error()
				}
				collections[kind][id] = base
				if c.History[key] == nil {
					c.History[key] = map[string][]string{}
				}
				for field, origin := range c.Provenance[key] {
					history := c.History[key][field]
					if len(history) == 0 || history[len(history)-1] != origin {
						c.History[key][field] = append(history, origin)
					}
				}
			}
		}
	}
	for kind, entities := range collections {
		for id, entity := range entities {
			key := kind + ":" + id
			var disabled bool
			_ = json.Unmarshal(entity["disabled"], &disabled)
			if disabled {
				continue
			}
			data, _ := json.Marshal(entity)
			reason := invalid[key]
			switch kind {
			case "skills":
				var skill domain.Skill
				if err := json.Unmarshal(data, &skill); err != nil {
					return c, err
				}
				check, _ := json.Marshal(struct {
					Version int                `json:"schemaVersion"`
					Skills  []domain.RawEntity `json:"skills"`
				}{2, []domain.RawEntity{entity}})
				if err := contracts.Validate("skills.effective.schema.json", check); err != nil {
					reason = err.Error()
				}
				if reason == "" {
					skill.RegistryID = c.Provenance[key]["source"]
					skill.Name = storage.Safe(skill.Name)
					skill.Description = storage.Safe(skill.Description)
					for i := range skill.Tags {
						skill.Tags[i] = storage.Safe(skill.Tags[i])
					}
					c.Skills[id] = skill
				}
			case "mappings":
				var mapping domain.Mapping
				_ = json.Unmarshal(data, &mapping)
				check, _ := json.Marshal(struct {
					Version  int                `json:"schemaVersion"`
					Mappings []domain.RawEntity `json:"mappings"`
				}{2, []domain.RawEntity{entity}})
				if err := contracts.Validate("mappings.effective.schema.json", check); err != nil {
					reason = err.Error()
				}
				if reason == "" {
					c.Mappings[id] = mapping
				}
			case "technologies":
				var technology domain.Technology
				_ = json.Unmarshal(data, &technology)
				if technology.Name == "" || technology.Category == "" || (!technology.MetadataOnly && len(technology.Rules) == 0) {
					reason = "incomplete effective technology"
				}
				if reason == "" {
					technology.Name = storage.Safe(technology.Name)
					c.Technologies[id] = technology
				} else {
					c.InvalidTechnologies[id] = reason
				}
			case "repositories":
				var repo domain.Repository
				_ = json.Unmarshal(data, &repo)
				if repo.Name == "" || repo.Kind == "" {
					reason = "incomplete repository"
				}
				if reason == "" {
					repo.Name = storage.Safe(repo.Name)
					c.Repositories[id] = repo
				}
			}
			if reason != "" {
				c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "E_EFFECTIVE_ENTITY", Severity: "error", Message: storage.Safe(reason), Subject: key})
			}
		}
	}
	semanticSkills(&c)
	for id, m := range c.Mappings {
		valid := true
		if _, ok := c.Technologies[m.TechnologyID]; !ok {
			valid = false
		}
		for _, r := range m.Recommendations {
			if _, ok := c.Skills[r.SkillID]; !ok {
				valid = false
			}
			for _, required := range append(slices.Clone(r.Requires), r.ConflictsWith...) {
				if _, ok := c.Skills[required]; !ok || required == r.SkillID {
					valid = false
				}
			}
			if r.When != nil {
				for _, t := range append(append(slices.Clone(r.When.All), r.When.Any...), r.When.None...) {
					if _, ok := c.Technologies[t]; !ok {
						valid = false
					}
				}
			}
		}
		if !valid {
			delete(c.Mappings, id)
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "E_MAPPING_REFERENCE", Severity: "error", Message: "Excluded mapping with an invalid effective reference", Subject: id})
		}
	}
	for kind, entities := range collections {
		c.Raw[kind] = map[string]json.RawMessage{}
		for id, entity := range entities {
			active := false
			switch kind {
			case "technologies":
				_, active = c.Technologies[id]
			case "skills":
				_, active = c.Skills[id]
			case "mappings":
				_, active = c.Mappings[id]
			case "repositories":
				_, active = c.Repositories[id]
			}
			if active {
				data, err := json.Marshal(entity)
				if err != nil {
					return c, err
				}
				c.Raw[kind][id] = data
			}
		}
	}
	return c, nil
}
