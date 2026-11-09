package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/dlclark/regexp2"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

const validationRegistryID = "validation:input"

func errorDiagnostic(code, message, subject string) domain.Diagnostic {
	return domain.Diagnostic{Code: code, Severity: "error", Message: storage.Safe(message), Subject: subject}
}

func hasErrors(diagnostics []domain.Diagnostic) bool {
	for i := range diagnostics {
		if diagnostics[i].Severity == "error" {
			return true
		}
	}
	return false
}

// overrides renders docs into the byte form the generator consumes.
func overrides(docs map[string]*document) (map[string][]byte, error) {
	out := make(map[string][]byte, len(docs))
	for role, doc := range docs {
		data, err := doc.encode(role)
		if err != nil {
			return nil, err
		}
		out[role] = data
	}
	return out, nil
}

// stagedCatalog validates ops against disk and loads the effective catalog from
// a temporary copy. The caller must invoke the returned cleanup.
func (s *Server) stagedCatalog(ctx context.Context, ops []Op) (domain.Catalog, []domain.Diagnostic, func(), error) {
	cleanup := func() {}
	diagnostics := []domain.Diagnostic{}
	docs, err := loadDocuments(s.view, s.cfg)
	if err != nil {
		return domain.Catalog{}, diagnostics, cleanup, err
	}
	conflicts, err := replay(ops, docs, s.automated)
	if err != nil {
		return domain.Catalog{}, diagnostics, cleanup, err
	}
	for _, conflict := range conflicts {
		diagnostics = append(diagnostics, domain.Diagnostic{Code: "W_STUDIO_CONFLICT", Severity: "warning", Message: conflict.Reason, Subject: conflict.ID})
	}
	data, err := overrides(docs)
	if err != nil {
		return domain.Catalog{}, diagnostics, cleanup, err
	}
	staged, err := registrygen.Stage(s.view, data)
	if err != nil {
		return domain.Catalog{}, append(diagnostics, errorDiagnostic("E_STUDIO_STAGE", err.Error(), "")), cleanup, nil
	}
	diagnostics = append(diagnostics, staged.Diagnostics...)
	diagnostics = append(diagnostics, ruleDiagnostics(ops)...)
	if hasErrors(staged.Diagnostics) {
		return domain.Catalog{}, diagnostics, cleanup, nil
	}
	dir, err := os.MkdirTemp("", "nosey-studio-")
	if err != nil {
		return domain.Catalog{}, diagnostics, cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if err = s.materialize(dir, staged, docs[roleSkills]); err != nil {
		return domain.Catalog{}, diagnostics, cleanup, err
	}
	catalog, err := s.effective(ctx, dir)
	if err != nil {
		return domain.Catalog{}, append(diagnostics, errorDiagnostic("E_STUDIO_LOAD", err.Error(), "")), cleanup, nil
	}
	return catalog, append(diagnostics, bareSubjects(catalog.Diagnostics)...), cleanup, nil
}

// bareSubjects drops the loader's "role:" subject prefix so diagnostics attach
// to the same entity ids the editor uses.
func bareSubjects(diagnostics []domain.Diagnostic) []domain.Diagnostic {
	out := make([]domain.Diagnostic, 0, len(diagnostics))
	for i := range diagnostics {
		diagnostic := diagnostics[i]
		diagnostic.Subject = strings.TrimPrefix(diagnostic.Subject, validationRegistryID+":")
		for _, role := range []string{roleSkills, roleTechnologies, roleMappings, "repositories"} {
			if rest, found := strings.CutPrefix(diagnostic.Subject, role+":"); found && rest != "" {
				diagnostic.Subject = rest
				break
			}
		}
		out = append(out, diagnostic)
	}
	return out
}

// materialize writes a staged generation so the runtime loader can read it.
func (s *Server) materialize(dir string, staged registrygen.Staged, skills *document) error {
	for name, data := range staged.Files {
		if err := storage.Write(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	if skills == nil {
		return nil
	}
	for _, entity := range skills.entities() {
		var skill struct {
			Source struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"source"`
		}
		if json.Unmarshal(entity, &skill) != nil || skill.Source.Kind != "local" {
			continue
		}
		name := skill.Source.Path + "/SKILL.md"
		if data, err := storage.Read(s.view, name, 1<<20); err == nil {
			if err = storage.Write(filepath.Join(dir, name), data); err != nil {
				return err
			}
		}
	}
	return nil
}

// effective applies the same standalone load as `registry validate --standalone`.
func (s *Server) effective(ctx context.Context, dir string) (domain.Catalog, error) {
	config := settings.Defaults()
	config.Registries = []domain.RegistryRef{{ID: validationRegistryID, Source: domain.RegistrySource{Kind: "local", Path: dir}, Enabled: true, Priority: 30}}
	resolved := settings.Resolved{WorkspaceIDs: map[string]bool{}, Settings: config}
	return registry.Service{Store: s.opts.Store}.Load(ctx, resolved, false)
}

func (s *Server) diagnostics(ctx context.Context, ops []Op) []domain.Diagnostic {
	_, diagnostics, cleanup, err := s.stagedCatalog(ctx, ops)
	defer cleanup()
	if err != nil {
		diagnostics = append(diagnostics, errorDiagnostic("E_STUDIO_VALIDATE", err.Error(), ""))
	}
	return diagnostics
}

// ruleDiagnostics catches matchers the schema accepts but detection cannot run.
func ruleDiagnostics(ops []Op) []domain.Diagnostic {
	found := []domain.Diagnostic{}
	for _, op := range ops {
		switch {
		case op.Entity == nil:
		case op.Role == roleTechnologies:
			found = append(found, technologyDiagnostics(op)...)
		case op.Role == roleMappings:
			var mapping struct {
				Recommendations []json.RawMessage `json:"recommendations"`
			}
			if json.Unmarshal(op.Entity, &mapping) == nil && len(mapping.Recommendations) == 0 {
				found = append(found, domain.Diagnostic{Code: "W_STUDIO_EMPTY_MAPPING", Severity: "warning", Message: "Mapping has no recommendations; delete it instead", Subject: op.ID})
			}
		}
	}
	return found
}

func technologyDiagnostics(op Op) []domain.Diagnostic {
	found := []domain.Diagnostic{}
	var technology struct {
		Rules []domain.Rule `json:"rules"`
	}
	if err := json.Unmarshal(op.Entity, &technology); err != nil {
		return append(found, errorDiagnostic("E_STUDIO_RULE", err.Error(), op.ID))
	}
	seen := map[string]bool{}
	for i := range technology.Rules {
		rule := &technology.Rules[i]
		if seen[rule.ID] {
			found = append(found, errorDiagnostic("E_STUDIO_RULE", "Duplicate rule id "+rule.ID, op.ID))
		}
		seen[rule.ID] = true
		for _, pattern := range []string{rule.PathRegex, rule.NameRegex, rule.Regex, rule.KeyRegex} {
			if pattern == "" {
				continue
			}
			if _, err := regexp2.Compile(pattern, regexp2.ECMAScript); err != nil {
				found = append(found, errorDiagnostic("E_STUDIO_RULE", fmt.Sprintf("Rule %s has an invalid regex: %v", rule.ID, err), op.ID))
			}
		}
		if rule.Glob != "" && !doublestar.ValidatePattern(rule.Glob) {
			found = append(found, errorDiagnostic("E_STUDIO_RULE", fmt.Sprintf("Rule %s has an invalid glob", rule.ID), op.ID))
		}
	}
	return found
}
