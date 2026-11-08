package registrygen

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

// InputInfo describes one data file and its resolved upstream revision.
type InputInfo struct {
	Path     string  `json:"path"`
	Revision *string `json:"revision"`
}

// Result reports one generation or refresh invocation.
type Result struct {
	Version      int                 `json:"outputVersion"`
	Command      string              `json:"command"`
	Path         string              `json:"path"`
	ChangedFiles []string            `json:"changedFiles"`
	RemovedFiles []string            `json:"removedFiles"`
	Inputs       []InputInfo         `json:"inputs"`
	Summary      Summary             `json:"summary"`
	Diagnostics  []domain.Diagnostic `json:"diagnostics"`
}

// Options controls one generation run. Check never writes. Overrides and
// Revisions are set by refresh for the files it fetched.
type Options struct {
	Check     bool
	Command   string
	Overrides map[string][]byte
	Revisions map[string]string
}

// Run canonicalizes declared data files and writes the manifest and lock.
// With Check it computes the same bytes and reports drift instead of writing.
func Run(root string, opts Options) (Result, error) {
	command := opts.Command
	if command == "" {
		command = "generate"
	}
	target, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Version:      1,
		Command:      command,
		Path:         target,
		ChangedFiles: []string{},
		RemovedFiles: []string{},
		Inputs:       []InputInfo{},
		Diagnostics:  []domain.Diagnostic{},
	}
	cfg, err := Load(target)
	if err != nil {
		return result, err
	}
	old, _, err := readLock(target)
	if err != nil {
		return result, err
	}
	docs := documents{}
	missing := []string{}
	for _, role := range sortedRoles(cfg.Files) {
		path := cfg.Files[role]
		data, ok := opts.Overrides[role]
		if !ok {
			data, err = storage.Read(target, path, 16<<20)
			if err != nil {
				if opts.Check && errors.Is(err, fs.ErrNotExist) {
					missing = append(missing, path)
					continue
				}
				return result, err
			}
		}
		doc, err := canonicalize(role, data)
		if err != nil {
			return result, err
		}
		docs[role] = doc
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return result, &DriftError{Paths: missing}
	}
	manifest, err := buildManifest(cfg)
	if err != nil {
		return result, err
	}
	if err = validateStaged(target, docs, manifest, cfg.Manifest, cfg.Templates); err != nil {
		return result, err
	}
	lock := buildLock(cfg, docs, manifest, old, opts.Revisions)
	lockBytes, err := lock.encode()
	if err != nil {
		return result, err
	}
	result.Summary = lock.Summary
	result.Inputs = inputInfo(cfg, lock)
	if opts.Check {
		paths, err := check(target, cfg, docs, manifest, lockBytes)
		if err != nil {
			return result, err
		}
		if len(paths) > 0 {
			return result, &DriftError{Paths: paths}
		}
		return result, nil
	}
	changed, err := write(target, cfg, docs, manifest, lockBytes)
	if err != nil {
		return result, err
	}
	removed := removeMovedManifest(target, old, cfg.Manifest)
	result.ChangedFiles = changed
	result.RemovedFiles = removed
	return result, nil
}

func sortedRoles(files map[string]string) []string {
	roles := make([]string, 0, len(files))
	for role := range files {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	return roles
}

// manifestOutput fixes top-level key order while nested maps stay sorted.
type manifestOutput struct {
	Version     int    `json:"schemaVersion"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Files       any    `json:"files"`
	Templates   any    `json:"templates,omitempty"`
	Scores      any    `json:"repositoryScores,omitempty"`
}

// buildManifest renders the manifest with a stable top-level field order and
// sorted nested object keys.
func buildManifest(cfg Config) ([]byte, error) {
	manifest := domain.Manifest{
		Version:     1,
		ID:          cfg.Registry.ID,
		Name:        cfg.Registry.Name,
		Description: cfg.Registry.Description,
		Files:       cfg.Files,
		Templates:   cfg.Templates,
		Scores:      cfg.RepositoryScores,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	if err = json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	output := manifestOutput{
		Version:     manifest.Version,
		ID:          manifest.ID,
		Name:        manifest.Name,
		Description: manifest.Description,
		Files:       generic["files"],
	}
	if templates, ok := generic["templates"]; ok {
		output.Templates = templates
	}
	if scores, ok := generic["repositoryScores"]; ok {
		output.Scores = scores
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func buildLock(cfg Config, docs documents, manifest []byte, old Lock, revisions map[string]string) Lock {
	lock := Lock{
		SchemaVersion: 1,
		Generator:     Generator,
		Files:         map[string]FileEntry{},
		Manifest:      ManifestEntry{Path: cfg.Manifest, SHA256: hashBytes(manifest)},
	}
	for _, role := range sortedRoles(cfg.Files) {
		path := cfg.Files[role]
		entry := FileEntry{SHA256: hashBytes(docs[role].canonical)}
		if revision, ok := revisions[role]; ok && revision != "" {
			entry.Revision = revision
		} else if upstream, ok := cfg.Upstreams[role]; ok {
			if fullRevision(upstream.Ref) {
				entry.Revision = upstream.Ref
			} else {
				entry.Revision = revisionFor(old, path)
			}
		}
		lock.Files[path] = entry
	}
	skills, technologies, mappings := docs.counts()
	lock.Summary = Summary{Skills: skills, Technologies: technologies, Mappings: mappings}
	return lock
}

func inputInfo(cfg Config, lock Lock) []InputInfo {
	inputs := make([]InputInfo, 0, len(cfg.Files))
	for _, role := range sortedRoles(cfg.Files) {
		path := cfg.Files[role]
		info := InputInfo{Path: path}
		if entry, ok := lock.Files[path]; ok && entry.Revision != "" {
			revision := entry.Revision
			info.Revision = &revision
		}
		inputs = append(inputs, info)
	}
	slices.SortFunc(inputs, func(a, b InputInfo) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		default:
			return 0
		}
	})
	return inputs
}

// check compares computed bytes with disk and returns drifted relative paths.
func check(root string, cfg Config, docs documents, manifest, lock []byte) ([]string, error) {
	drift := []string{}
	for _, role := range sortedRoles(cfg.Files) {
		path := cfg.Files[role]
		data, err := storage.Read(root, path, 16<<20)
		if err != nil || !bytes.Equal(data, docs[role].canonical) {
			drift = append(drift, path)
		}
	}
	if data, err := storage.Read(root, cfg.Manifest, 16<<20); err != nil || !bytes.Equal(data, manifest) {
		drift = append(drift, cfg.Manifest)
	}
	if data, err := storage.Read(root, LockName, 1<<20); err != nil || !bytes.Equal(data, lock) {
		drift = append(drift, LockName)
	}
	slices.Sort(drift)
	return slices.Compact(drift), nil
}

// write replaces only files whose bytes changed, manifest before lock.
func write(root string, cfg Config, docs documents, manifest, lock []byte) ([]string, error) {
	changed := []string{}
	for _, role := range sortedRoles(cfg.Files) {
		path := cfg.Files[role]
		data := docs[role].canonical
		if current, err := storage.Read(root, path, 16<<20); err == nil && bytes.Equal(current, data) {
			continue
		}
		if err := storage.Write(filepath.Join(root, path), data); err != nil {
			return changed, err
		}
		changed = append(changed, path)
	}
	if current, err := storage.Read(root, cfg.Manifest, 1<<20); err != nil || !bytes.Equal(current, manifest) {
		if err = storage.Write(filepath.Join(root, cfg.Manifest), manifest); err != nil {
			return changed, err
		}
		changed = append(changed, cfg.Manifest)
	}
	if current, err := storage.Read(root, LockName, 1<<20); err != nil || !bytes.Equal(current, lock) {
		if err = storage.Write(filepath.Join(root, LockName), lock); err != nil {
			return changed, err
		}
		changed = append(changed, LockName)
	}
	slices.Sort(changed)
	return changed, nil
}

// removeMovedManifest deletes the previous manifest only when its path changed.
func removeMovedManifest(root string, old Lock, current string) []string {
	if old.Manifest.Path == "" || old.Manifest.Path == current {
		return []string{}
	}
	if err := os.Remove(filepath.Join(root, old.Manifest.Path)); err != nil {
		return []string{}
	}
	return []string{old.Manifest.Path}
}
