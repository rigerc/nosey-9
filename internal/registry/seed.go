package registry

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

// seedWorkspace retains the selected mappings and their complete reference closure.
func seedWorkspace(stage string, manifest domain.Manifest, c domain.Catalog, include []string) error {
	techs, skills, mappings, repos := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, id := range include {
		if _, ok := c.Technologies[id]; !ok {
			return fmt.Errorf("unknown selected technology %s", id)
		}
		techs[id] = true
		for mid, m := range c.Mappings {
			if m.TechnologyID != id {
				continue
			}
			mappings[mid] = true
			for _, r := range m.Recommendations {
				skills[r.SkillID] = true
				for _, dep := range append(slices.Clone(r.Requires), r.ConflictsWith...) {
					skills[dep] = true
				}
				if r.When != nil {
					for _, t := range append(append(slices.Clone(r.When.All), r.When.Any...), r.When.None...) {
						techs[t] = true
					}
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for id := range skills {
			skill, ok := c.Skills[id]
			if !ok {
				return fmt.Errorf("missing skill reference %s", id)
			}
			for _, dep := range append(slices.Clone(skill.Requires), skill.ConflictsWith...) {
				if !skills[dep] {
					skills[dep] = true
					changed = true
				}
			}
			for _, t := range skill.Technologies {
				techs[t] = true
			}
			if skill.RepositoryID != "" {
				repos[skill.RepositoryID] = true
			}
		}
	}
	sets := map[string]map[string]bool{"technologies": techs, "skills": skills, "mappings": mappings, "repositories": repos}
	for kind, set := range sets {
		if manifest.Files[kind] == "" {
			if len(set) > 0 {
				return fmt.Errorf("template has no %s output", kind)
			}
			continue
		}
		ids := []string{}
		for id := range set {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		entities := []json.RawMessage{}
		for _, id := range ids {
			raw := c.Raw[kind][id]
			if len(raw) == 0 {
				return fmt.Errorf("missing effective %s reference %s", kind, id)
			}
			if kind == "skills" && c.Skills[id].Source.Kind == "local" {
				skill := c.Skills[id]
				source, revision := c.Sources[skill.RegistryID], c.Revisions[skill.RegistryID]
				if source.Kind != "git" || revision == nil {
					return fmt.Errorf("workspace seed requires a portable Git source for %s", id)
				}
				var entity domain.RawEntity
				if err := json.Unmarshal(raw, &entity); err != nil {
					return err
				}
				data, err := json.Marshal(domain.Source{Kind: "git", URI: source.URI, Ref: *revision, SkillName: skill.Source.SkillName})
				if err != nil {
					return err
				}
				entity["source"] = data
				raw, err = json.Marshal(entity)
				if err != nil {
					return err
				}
			}
			entities = append(entities, raw)
		}
		doc := map[string]json.RawMessage{"schemaVersion": json.RawMessage(`2`)}
		if kind == "repositories" {
			doc["schemaVersion"] = json.RawMessage(`1`)
		}
		data, err := json.Marshal(entities)
		if err != nil {
			return err
		}
		doc[kind] = data
		if err = storage.JSON(filepath.Join(stage, manifest.Files[kind]), doc); err != nil {
			return err
		}
	}
	return nil
}
