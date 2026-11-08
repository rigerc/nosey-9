package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

// InitOptions selects the published empty template and explicit output actions.
type InitOptions struct {
	Path, RegistryID, Version string
	// CI adds the template's GitHub Actions workflow.
	CI bool
	// GitHubRepo, as OWNER/NAME, creates and pushes a GitHub repository with gh.
	GitHubRepo string
	// Public makes the GitHub repository public; it is private otherwise.
	Public        bool
	Register, Git bool
}

var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)

// ValidGitHubRepo reports whether repo is an OWNER/NAME GitHub repository slug.
func ValidGitHubRepo(repo string) bool { return githubRepoPattern.MatchString(repo) }

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

type templateDescriptor struct {
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

// Init stages and validates data-only template output before publication.
func (s Service) Init(ctx context.Context, c domain.Catalog, opts InitOptions) (InitResult, error) {
	const templateID = "empty"
	result := InitResult{Version: 2, Diagnostics: []domain.Diagnostic{}, Files: []string{}, TemplateID: templateID, SourceRegistryID: opts.RegistryID, CreatedBy: opts.Version}
	if result.SourceRegistryID == "" {
		result.SourceRegistryID = "nosey:official"
	}
	if opts.GitHubRepo != "" && !ValidGitHubRepo(opts.GitHubRepo) {
		return result, fmt.Errorf("invalid GitHub repository %q (expected OWNER/NAME)", opts.GitHubRepo)
	}
	if opts.Public && opts.GitHubRepo == "" {
		return result, fmt.Errorf("public visibility requires a GitHub repository")
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
	index := slices.IndexFunc(manifest.Templates, func(t domain.Template) bool { return t.ID == templateID && t.Kind == "registry" })
	if index < 0 {
		return result, fmt.Errorf("template %s not published by registry %s", templateID, result.SourceRegistryID)
	}
	descriptor := manifest.Templates[index].Descriptor
	bytes, err := storage.Read(root, descriptor, 1<<20)
	if err != nil {
		return result, err
	}
	if err = contracts.Validate("template.schema.json", bytes); err != nil {
		return result, err
	}
	var template templateDescriptor
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
	base := filepath.Dir(descriptor)
	seen := map[string]bool{}
	total := 0
	copyFile := func(source, destination string) error {
		if !filepath.IsLocal(destination) || !filepath.IsLocal(source) || seen[destination] || strings.Contains(destination, "\\") || strings.Contains(source, "\\") {
			return fmt.Errorf("unsafe or duplicate template path")
		}
		seen[destination] = true
		bytes, err := storage.Read(root, filepath.Join(base, source), 1<<20)
		if err != nil {
			return err
		}
		total += len(bytes)
		if total > 8<<20 || len(seen) > 100 {
			return fmt.Errorf("template resource limit exceeded")
		}
		return storage.Write(filepath.Join(stage, destination), bytes)
	}
	for _, file := range template.Files {
		if err = copyFile(file.Source, file.Destination); err != nil {
			return result, err
		}
		result.Files = append(result.Files, file.Destination)
	}
	ciProviders := []string{}
	if opts.CI {
		for _, ci := range template.CI {
			if ci.Provider != "github" {
				return result, fmt.Errorf("unsupported CI provider %s", ci.Provider)
			}
			if err = copyFile(ci.Source, ci.Destination); err != nil {
				return result, err
			}
			result.Files = append(result.Files, ci.Destination)
			ciProviders = append(ciProviders, ci.Provider)
		}
	}
	if _, err = os.Stat(filepath.Join(stage, registrygen.ConfigName)); err != nil {
		return result, fmt.Errorf("template registry %s provides an outdated %q template without %s; run `nosey registry update` to refresh it", result.SourceRegistryID, templateID, registrygen.ConfigName)
	}
	id := "local:" + regexp.MustCompile(`[^A-Za-z0-9._:-]+`).ReplaceAllString(filepath.Base(target), "-")
	if err = rewriteConfig(stage, id, filepath.Base(target)); err != nil {
		return result, err
	}
	result.RegistryID = id
	if _, err = registrygen.Run(stage, registrygen.Options{Command: "generate"}); err != nil {
		return result, err
	}
	generated, err := readManifest(stage)
	if err != nil {
		return result, err
	}
	scaffold := struct {
		Version   int      `json:"schemaVersion"`
		Template  string   `json:"templateId"`
		Source    string   `json:"sourceRegistryId"`
		Revision  *string  `json:"sourceRevision"`
		CreatedBy string   `json:"createdBy"`
		CI        []string `json:"ci,omitempty"`
	}{1, templateID, result.SourceRegistryID, result.Revision, opts.Version, ciProviders}
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
	switch {
	case opts.GitHubRepo != "":
		if err = s.publishGitHub(ctx, target, opts); err != nil {
			return result, fmt.Errorf("registry created; GitHub repository creation failed: %w", err)
		}
	case opts.Git:
		if _, err = s.run(ctx, target, nil, "git", "init"); err != nil {
			return result, fmt.Errorf("registry created; optional git init failed: %w", err)
		}
	}
	return result, nil
}

// publishGitHub commits the new registry and creates a GitHub repository from it.
// It requires git with a configured identity and an authenticated gh CLI.
func (s Service) publishGitHub(ctx context.Context, dir string, opts InitOptions) error {
	visibility := "--private"
	if opts.Public {
		visibility = "--public"
	}
	steps := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "Initial registry"},
		{"gh", "repo", "create", opts.GitHubRepo, visibility, "--source", ".", "--remote", "origin", "--push"},
	}
	for _, step := range steps {
		if _, err := s.run(ctx, dir, nil, step[0], step[1:]...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(step[:min(len(step), 2)], " "), err)
		}
	}
	return nil
}

// rewriteConfig sets the staged registry identity and removes any seed source.
func rewriteConfig(stage, id, name string) error {
	data, err := storage.Read(stage, registrygen.ConfigName, 1<<20)
	if err != nil {
		return err
	}
	var config registrygen.Config
	if err = json.Unmarshal(data, &config); err != nil {
		return err
	}
	config.Registry.ID = id
	config.Registry.Name = name
	config.Registry.Description = ""
	config.Source = nil
	return storage.JSON(filepath.Join(stage, registrygen.ConfigName), config)
}
