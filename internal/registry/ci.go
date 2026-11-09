package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	scaffoldName    = "nosey.scaffold.json"
	scaffoldVersion = 2
	ciDirectory     = ".github/"
	ciProvider      = "github"
)

var generatorRefPattern = regexp.MustCompile(`(?m)^\s*GENERATOR_REF:\s*([0-9a-f]{40})\s*$`)

// scaffold is the provenance recorded in a registry made by Init.
type scaffold struct {
	Version      int               `json:"schemaVersion"`
	Template     string            `json:"templateId"`
	Source       string            `json:"sourceRegistryId"`
	Revision     *string           `json:"sourceRevision"`
	CreatedBy    string            `json:"createdBy"`
	CI           []string          `json:"ci,omitempty"`
	CIFiles      map[string]string `json:"ciFiles,omitempty"`
	GeneratorRef string            `json:"generatorRef,omitempty"`
}

// recordCI notes a CI file the scaffold now owns, so an update can tell a stale
// copy from a local edit.
func (r *scaffold) recordCI(provider, destination string, data []byte) {
	if !slices.Contains(r.CI, provider) {
		r.CI = append(r.CI, provider)
	}
	if r.CIFiles == nil {
		r.CIFiles = map[string]string{}
	}
	r.CIFiles[destination] = storage.Hash(data)
	if match := generatorRefPattern.FindSubmatch(data); match != nil {
		r.GeneratorRef = string(match[1])
	}
}

// checkCIFile keeps template CI files inside .github, so a template cannot
// write Git hooks or other executable paths into a registry.
func checkCIFile(provider, destination string) error {
	if provider != ciProvider {
		return fmt.Errorf("unsupported CI provider %s", provider)
	}
	if !filepath.IsLocal(destination) || !strings.HasPrefix(filepath.Clean(destination), ciDirectory) || strings.Contains(destination, "\\") {
		return fmt.Errorf("unsafe CI file path %q", destination)
	}
	return nil
}

func readTemplate(root, descriptor string) (templateDescriptor, error) {
	var template templateDescriptor
	data, err := storage.Read(root, descriptor, 1<<20)
	if err != nil {
		return template, err
	}
	if err = contracts.Validate("template.schema.json", data); err != nil {
		return template, err
	}
	if err = json.Unmarshal(data, &template); err != nil {
		return template, err
	}
	if template.Generator != "registry-v1" {
		return template, fmt.Errorf("unsupported template generator")
	}
	return template, nil
}

// CIUpdateOptions selects a registry and how to treat local edits.
type CIUpdateOptions struct {
	Path string
	// RegistryID overrides the template provider recorded at init.
	RegistryID string
	// Force overwrites CI files that were edited locally.
	Force bool
}

// CIUpdateResult lists what changed. Kept files left the template but were edited locally.
type CIUpdateResult struct {
	Path, SourceRegistryID             string
	Added, Updated, Unchanged, Removed []string
	Kept                               []string
	GeneratorFrom, GeneratorTo         string
}

type ciFile struct {
	destination string
	data        []byte
}

func readCIFiles(root, base string, template templateDescriptor) ([]ciFile, error) {
	files := make([]ciFile, 0, len(template.CI))
	for _, ci := range template.CI {
		if err := checkCIFile(ci.Provider, ci.Destination); err != nil {
			return nil, err
		}
		if !filepath.IsLocal(ci.Source) || strings.Contains(ci.Source, "\\") {
			return nil, fmt.Errorf("unsafe template path %q", ci.Source)
		}
		data, err := storage.Read(root, filepath.Join(base, ci.Source), 1<<20)
		if err != nil {
			return nil, err
		}
		files = append(files, ciFile{ci.Destination, data})
	}
	return files, nil
}

func readScaffold(target string) (scaffold, error) {
	var record scaffold
	data, err := storage.Read(target, scaffoldName, 1<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return record, fmt.Errorf("%s has no %s; only registries made by `nosey registry init` can be updated", target, scaffoldName)
	}
	if err != nil {
		return record, err
	}
	if err = contracts.Validate("scaffold.schema.json", data); err != nil {
		return record, fmt.Errorf("%s: %w", scaffoldName, err)
	}
	return record, json.Unmarshal(data, &record)
}

func loadCITemplate(c domain.Catalog, source, templateID string) ([]ciFile, error) {
	root, ok := c.Roots[source]
	if !ok {
		return nil, fmt.Errorf("template registry %s is not available", source)
	}
	manifest, err := readManifest(root)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(manifest.Templates, func(t domain.Template) bool { return t.ID == templateID && t.Kind == "registry" })
	if index < 0 {
		return nil, fmt.Errorf("template %s not published by registry %s", templateID, source)
	}
	descriptor := manifest.Templates[index].Descriptor
	template, err := readTemplate(root, descriptor)
	if err != nil {
		return nil, err
	}
	return readCIFiles(root, filepath.Dir(descriptor), template)
}

// UpdateCI brings a scaffolded registry's CI files up to the current template.
// Nothing is written while any file has unreviewed local edits.
func (s Service) UpdateCI(c domain.Catalog, opts CIUpdateOptions) (CIUpdateResult, error) {
	result := CIUpdateResult{}
	target, err := filepath.Abs(opts.Path)
	if err != nil {
		return result, err
	}
	record, err := readScaffold(target)
	if err != nil {
		return result, err
	}
	result.Path, result.SourceRegistryID = target, record.Source
	if opts.RegistryID != "" {
		result.SourceRegistryID = opts.RegistryID
	}
	files, err := loadCITemplate(c, result.SourceRegistryID, record.Template)
	if err != nil {
		return result, err
	}
	writes, conflicts, err := planCI(target, record, files, opts.Force, &result)
	if err != nil {
		return result, err
	}
	if len(conflicts) > 0 {
		return result, fmt.Errorf("edited locally: %s; review them, then rerun with --force to replace them", strings.Join(conflicts, ", "))
	}
	return result, applyCI(target, record, files, writes, &result)
}

// planCI classifies every template file without writing anything.
func planCI(target string, record scaffold, files []ciFile, force bool, result *CIUpdateResult) (writes []ciFile, conflicts []string, err error) {
	for _, file := range files {
		current, readErr := storage.Read(target, file.destination, 1<<20)
		recorded, known := record.CIFiles[file.destination]
		missing := errors.Is(readErr, fs.ErrNotExist)
		switch {
		case missing && (!known || force):
			result.Added = append(result.Added, file.destination)
			writes = append(writes, file)
		case missing:
			conflicts = append(conflicts, file.destination)
		case readErr != nil:
			return nil, nil, readErr
		case bytes.Equal(current, file.data):
			result.Unchanged = append(result.Unchanged, file.destination)
		case force || recorded == storage.Hash(current):
			result.Updated = append(result.Updated, file.destination)
			writes = append(writes, file)
		default:
			conflicts = append(conflicts, file.destination)
		}
	}
	return writes, conflicts, nil
}

func applyCI(target string, record scaffold, files, writes []ciFile, result *CIUpdateResult) error {
	for _, file := range writes {
		if err := storage.WriteAt(target, file.destination, file.data); err != nil {
			return err
		}
	}
	if err := removeRetiredCI(target, record, files, result); err != nil {
		return err
	}
	result.GeneratorFrom = record.GeneratorRef
	next := record
	next.Version, next.CI, next.CIFiles, next.GeneratorRef = scaffoldVersion, nil, nil, ""
	for _, file := range files {
		next.recordCI(ciProvider, file.destination, file.data)
	}
	result.GeneratorTo = next.GeneratorRef
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	// Written last: a failed run leaves the old record, and files already
	// replaced then compare equal to the template on retry.
	return storage.WriteAt(target, scaffoldName, append(encoded, '\n'))
}

// removeRetiredCI deletes files the template no longer ships, unless edited.
func removeRetiredCI(target string, record scaffold, files []ciFile, result *CIUpdateResult) error {
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, destination := range slices.Sorted(maps.Keys(record.CIFiles)) {
		if slices.ContainsFunc(files, func(file ciFile) bool { return file.destination == destination }) {
			continue
		}
		current, readErr := storage.Read(target, destination, 1<<20)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}
		if readErr != nil || checkCIFile(ciProvider, destination) != nil || storage.Hash(current) != record.CIFiles[destination] {
			result.Kept = append(result.Kept, destination)
			continue
		}
		if err = root.Remove(destination); err != nil {
			return err
		}
		result.Removed = append(result.Removed, destination)
	}
	return nil
}
