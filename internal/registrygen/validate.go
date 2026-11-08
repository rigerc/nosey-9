package registrygen

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

// validateDocuments applies the published runtime schema to each canonical document.
func validateDocuments(docs documents) error {
	roles := make([]string, 0, len(docs))
	for role := range docs {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	for _, role := range roles {
		doc := docs[role]
		schema, err := schemaFor(role, doc.version)
		if err != nil {
			return inputError("%s: %w", role, err)
		}
		if err = contracts.Validate(schema, doc.canonical); err != nil {
			return inputError("%s: %w", role, err)
		}
	}
	return nil
}

type skillRef struct {
	ID            string   `json:"id"`
	Technologies  []string `json:"technologies"`
	Requires      []string `json:"requires"`
	ConflictsWith []string `json:"conflictsWith"`
	RepositoryID  string   `json:"repositoryId"`
}

type recommendationRef struct {
	SkillID       string   `json:"skillId"`
	Requires      []string `json:"requires"`
	ConflictsWith []string `json:"conflictsWith"`
	When          *struct {
		All  []string `json:"allTechnologies"`
		Any  []string `json:"anyTechnologies"`
		None []string `json:"noneTechnologies"`
	} `json:"when"`
}

type mappingRef struct {
	ID              string              `json:"id"`
	TechnologyID    string              `json:"technologyId"`
	Recommendations []recommendationRef `json:"recommendations"`
}

// validateReferences rejects cross-document references that cannot resolve.
func validateReferences(docs documents) error {
	technologies := idSet(docs["technologies"])
	skills := map[string]bool{}
	for _, entity := range entitiesOf(docs["skills"]) {
		var skill skillRef
		if err := json.Unmarshal(entity, &skill); err != nil {
			return inputError("skills: %w", err)
		}
		skills[skill.ID] = true
	}
	repositories := idSet(docs["repositories"])
	for _, entity := range entitiesOf(docs["skills"]) {
		var skill skillRef
		if err := json.Unmarshal(entity, &skill); err != nil {
			return inputError("skills: %w", err)
		}
		if skill.RepositoryID != "" && !repositories[skill.RepositoryID] {
			return inputError("skill %s references unknown repository %s", skill.ID, skill.RepositoryID)
		}
		for _, technology := range skill.Technologies {
			if !technologies[technology] {
				return inputError("skill %s references unknown technology %s", skill.ID, technology)
			}
		}
		for _, required := range skill.Requires {
			if !skills[required] {
				return inputError("skill %s requires unknown skill %s", skill.ID, required)
			}
			if required == skill.ID {
				return inputError("skill %s requires itself", skill.ID)
			}
			if slices.Contains(skill.ConflictsWith, required) {
				return inputError("skill %s both requires and conflicts with %s", skill.ID, required)
			}
		}
		for _, other := range skill.ConflictsWith {
			if !skills[other] || other == skill.ID {
				return inputError("skill %s conflicts with unknown or self skill %s", skill.ID, other)
			}
		}
	}
	for _, entity := range entitiesOf(docs["mappings"]) {
		var mapping mappingRef
		if err := json.Unmarshal(entity, &mapping); err != nil {
			return inputError("mappings: %w", err)
		}
		if mapping.TechnologyID == "" || !technologies[mapping.TechnologyID] {
			return inputError("mapping %s references unknown technology %q", mapping.ID, mapping.TechnologyID)
		}
		seen := map[string]bool{}
		for _, recommendation := range mapping.Recommendations {
			if seen[recommendation.SkillID] {
				return inputError("mapping %s recommends skill %s twice", mapping.ID, recommendation.SkillID)
			}
			seen[recommendation.SkillID] = true
			if !skills[recommendation.SkillID] {
				return inputError("mapping %s recommends unknown skill %s", mapping.ID, recommendation.SkillID)
			}
			for _, required := range append(slices.Clone(recommendation.Requires), recommendation.ConflictsWith...) {
				if !skills[required] || required == recommendation.SkillID {
					return inputError("mapping %s recommendation %s references unknown or self skill %s", mapping.ID, recommendation.SkillID, required)
				}
			}
			if recommendation.When == nil {
				continue
			}
			for _, technology := range append(append(slices.Clone(recommendation.When.All), recommendation.When.Any...), recommendation.When.None...) {
				if !technologies[technology] {
					return inputError("mapping %s recommendation %s references unknown technology %s", mapping.ID, recommendation.SkillID, technology)
				}
			}
		}
	}
	return nil
}

func entitiesOf(doc *document) []json.RawMessage {
	if doc == nil {
		return nil
	}
	return doc.entities
}

func idSet(doc *document) map[string]bool {
	set := map[string]bool{}
	for _, entity := range entitiesOf(doc) {
		var header struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(entity, &header) == nil && header.ID != "" {
			set[header.ID] = true
		}
	}
	return set
}

// validateTemplates checks every declared descriptor and every file it references.
func validateTemplates(root string, templates []domain.Template) error {
	seen := map[string]bool{}
	for _, template := range templates {
		if seen[template.ID] {
			return inputError("duplicate template id %q", template.ID)
		}
		seen[template.ID] = true
		data, err := storage.Read(root, template.Descriptor, 1<<20)
		if err != nil {
			return inputError("template %s: %w", template.ID, err)
		}
		if err = contracts.Validate("template.schema.json", data); err != nil {
			return inputError("template %s: %w", template.ID, err)
		}
		var descriptor struct {
			Generator string `json:"generator"`
			Files     []struct {
				Source      string `json:"source"`
				Destination string `json:"destination"`
			} `json:"files"`
			CI []struct {
				Provider    string `json:"provider"`
				Source      string `json:"source"`
				Destination string `json:"destination"`
			} `json:"ci"`
		}
		if err = json.Unmarshal(data, &descriptor); err != nil {
			return inputError("template %s: %w", template.ID, err)
		}
		if descriptor.Generator != "registry-v1" {
			return inputError("template %s: unsupported generator %q", template.ID, descriptor.Generator)
		}
		destinations := map[string]bool{}
		base := filepath.Dir(template.Descriptor)
		for _, file := range descriptor.Files {
			if err = checkTemplateFile(root, template.ID, base, file.Source, file.Destination, destinations); err != nil {
				return err
			}
		}
		for _, ci := range descriptor.CI {
			if ci.Provider != "github" {
				return inputError("template %s: unsupported CI provider %q", template.ID, ci.Provider)
			}
			if err = checkTemplateFile(root, template.ID, base, ci.Source, ci.Destination, destinations); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkTemplateFile(root, templateID, base, source, destination string, seen map[string]bool) error {
	if !filepath.IsLocal(destination) || strings.Contains(destination, "\\") {
		return inputError("template %s: unsafe destination %q", templateID, destination)
	}
	if seen[destination] {
		return inputError("template %s: duplicate destination %q", templateID, destination)
	}
	seen[destination] = true
	if _, err := storage.Read(root, filepath.Join(base, source), 16<<20); err != nil {
		return inputError("template %s: %w", templateID, err)
	}
	return nil
}

// validateManifest applies the published manifest schema.
func validateManifest(name string, data []byte) error {
	if err := contracts.Validate("registry.schema.json", data); err != nil {
		return inputError("%s: %w", name, err)
	}
	return nil
}

// validateStaged runs every in-memory check applied before any write.
func validateStaged(root string, docs documents, manifest []byte, manifestPath string, templates []domain.Template) error {
	if err := validateDocuments(docs); err != nil {
		return err
	}
	if err := validateReferences(docs); err != nil {
		return err
	}
	if err := validateManifest(manifestPath, manifest); err != nil {
		return err
	}
	return validateTemplates(root, templates)
}
