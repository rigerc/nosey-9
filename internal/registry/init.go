package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// InitOptions selects an external template and explicit output actions.
type InitOptions struct {
	Path, Template, RegistryID, Version string
	Include                             []string
	Register, Git                       bool
}

// InitResult is the versioned registry-generation envelope.
type InitResult struct {
	Version          int                 `json:"outputVersion"`
	Diagnostics      []domain.Diagnostic `json:"diagnostics"`
	Path             string              `json:"path"`
	RegistryID       string              `json:"registryId"`
	TemplateID       string              `json:"templateId"`
	SourceRegistryID string              `json:"sourceRegistryId"`
	Revision         *string             `json:"sourceRevision"`
	Files            []string            `json:"createdFiles"`
	Valid            bool                `json:"valid"`
	CreatedBy        string              `json:"createdBy"`
}

// Init stages and validates data-only template output before publication.
func (s Service) Init(ctx context.Context, c domain.Catalog, opts InitOptions) (InitResult, error) {
	result := InitResult{Version: 2, Diagnostics: []domain.Diagnostic{}, Files: []string{}, TemplateID: opts.Template, SourceRegistryID: opts.RegistryID, CreatedBy: opts.Version}
	if result.SourceRegistryID == "" {
		result.SourceRegistryID = "nosey:official"
	}
	root, ok := c.Roots[result.SourceRegistryID]
	if !ok {
		return result, fmt.Errorf("mandatory template registry unavailable")
	}
	manifest, err := readManifest(root)
	if err != nil {
		return result, err
	}
	result.Revision = c.Revisions[result.SourceRegistryID]
	index := slices.IndexFunc(manifest.Templates, func(t domain.Template) bool { return t.ID == opts.Template && t.Kind == "registry" })
	if index < 0 {
		return result, fmt.Errorf("template %s not published by registry %s", opts.Template, result.SourceRegistryID)
	}
	descriptor := manifest.Templates[index].Descriptor
	bytes, err := storage.Read(root, descriptor, 1<<20)
	if err != nil {
		return result, err
	}
	if err = contracts.Validate("template.schema.json", bytes); err != nil {
		return result, err
	}
	var template struct {
		Generator string `json:"generator"`
		Files     []struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
		} `json:"files"`
	}
	if err = json.Unmarshal(bytes, &template); err != nil {
		return result, err
	}
	if template.Generator != "registry-v1" {
		return result, fmt.Errorf("unsupported template generator")
	}
	target, err := filepath.Abs(opts.Path)
	if err != nil {
		return result, err
	}
	if _, err = os.Lstat(target); err == nil {
		return result, fmt.Errorf("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".nosey-init-")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	seen := map[string]bool{}
	total := 0
	for _, f := range template.Files {
		if !filepath.IsLocal(f.Destination) || !filepath.IsLocal(f.Source) || seen[f.Destination] || strings.Contains(f.Destination, "\\") || strings.Contains(f.Source, "\\") {
			return result, fmt.Errorf("unsafe or duplicate template path")
		}
		seen[f.Destination] = true
		bytes, err = storage.Read(root, filepath.Join(filepath.Dir(descriptor), f.Source), 1<<20)
		if err != nil {
			return result, err
		}
		total += len(bytes)
		if total > 8<<20 || len(seen) > 100 {
			return result, fmt.Errorf("template resource limit exceeded")
		}
		if err = storage.Write(filepath.Join(stage, f.Destination), bytes); err != nil {
			return result, err
		}
		result.Files = append(result.Files, f.Destination)
	}
	generated, err := readManifest(stage)
	if err != nil {
		return result, err
	}
	generated.ID = "local:" + regexp.MustCompile(`[^A-Za-z0-9._:-]+`).ReplaceAllString(filepath.Base(target), "-")
	generated.Name = filepath.Base(target)
	if err = storage.JSON(filepath.Join(stage, "nosey.registry.json"), generated); err != nil {
		return result, err
	}
	result.RegistryID = generated.ID
	if opts.Template == "workspace" && len(opts.Include) > 0 {
		if err = seedWorkspace(stage, generated, c, opts.Include); err != nil {
			return result, err
		}
	}
	scaffold := struct {
		Version   int     `json:"schemaVersion"`
		Template  string  `json:"templateId"`
		Source    string  `json:"sourceRegistryId"`
		Revision  *string `json:"sourceRevision"`
		CreatedBy string  `json:"createdBy"`
	}{1, opts.Template, result.SourceRegistryID, result.Revision, opts.Version}
	if err = storage.JSON(filepath.Join(stage, "nosey.scaffold.json"), scaffold); err != nil {
		return result, err
	}
	result.Files = append(result.Files, "nosey.scaffold.json")
	if err = validateSource(stage); err != nil {
		return result, err
	}
	checkSettings := settings.Defaults()
	checkSettings.Registries = []domain.RegistryRef{{ID: generated.ID, Source: domain.RegistrySource{Kind: "local", Path: stage}, Enabled: true}}
	checked, err := s.Load(ctx, settings.Resolved{Settings: checkSettings}, false)
	if err != nil {
		return result, err
	}
	for _, diagnostic := range checked.Diagnostics {
		if diagnostic.Severity == "error" {
			return result, fmt.Errorf("generated registry is invalid: %s", diagnostic.Message)
		}
	}
	if err = os.Rename(stage, target); err != nil {
		return result, err
	}
	result.Path = target
	result.Valid = true
	slices.Sort(result.Files)
	if opts.Git {
		if _, err = s.run(ctx, target, nil, "git", "init"); err != nil {
			return result, fmt.Errorf("registry created; optional git init failed: %w", err)
		}
	}
	if opts.Register {
		refs, err := s.Store.UserRegistries()
		if err != nil {
			return result, err
		}
		refs = append(refs, domain.RegistryRef{ID: generated.ID, Source: domain.RegistrySource{Kind: "local", Path: target}, Enabled: true, Priority: 20})
		if err = s.Store.Registries(refs); err != nil {
			return result, fmt.Errorf("registry created; optional registration failed: %w", err)
		}
	}
	return result, nil
}
