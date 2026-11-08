package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// lockEntry mirrors the project-scoped skills-lock.json entry (LocalSkillLockEntry in skills@1.7.6
// src/local-lock.ts). The global ~/.agents/.skill-lock.json (skill-lock.ts) is never read: Nosey is project-only.
type lockEntry struct {
	Source      string `json:"source"`
	SourceURL   string `json:"sourceUrl"`
	Type        string `json:"sourceType"`
	Ref         string `json:"ref"`
	SkillPath   string `json:"skillPath"`
	Hash        string `json:"computedHash"`
	InstallMode string `json:"installMode"`
}

// method reports the install method the lock recorded; upstream omits installMode for symlink installs.
func (e lockEntry) method() string {
	if e.InstallMode == "copy" {
		return "copy"
	}
	return "symlink"
}

type projectLock struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

// LockError describes why skills-lock.json cannot be used; Reason is safe to show to users.
type LockError struct{ Reason string }

func (e LockError) Error() string { return "skills-lock.json " + e.Reason }

func (s Service) readLock() (projectLock, error) {
	var lock projectLock
	data, err := storage.Read(s.Store.Workspace, "skills-lock.json", 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return lock, LockError{"is missing"}
	}
	if err != nil {
		return lock, LockError{"cannot be read: " + err.Error()}
	}
	if err = json.Unmarshal(data, &lock); err != nil {
		reason := "is not valid JSON"
		if strings.Contains(string(data), "<<<<<<<") {
			reason += " (unresolved merge conflict markers)"
		}
		return projectLock{}, LockError{reason}
	}
	if lock.Version != 1 {
		return projectLock{}, LockError{fmt.Sprintf("has unsupported version %d (expected 1)", lock.Version)}
	}
	if lock.Skills == nil {
		return projectLock{}, LockError{`has no "skills" map`}
	}
	return lock, nil
}

// validSkillPath accepts the upstream-recorded SKILL.md location: empty (local paths) or a relative path to SKILL.md.
func validSkillPath(path string) bool {
	return path == "" || filepath.IsLocal(path) && !strings.Contains(path, "\\") && filepath.Base(path) == "SKILL.md"
}
func (s Service) readReceipts() []receipt {
	data, err := storage.Read(s.Store.Workspace, ".nosey/install-origins.json", 4<<20)
	if err != nil || contracts.Validate("install-origins.schema.json", data) != nil {
		return nil
	}
	var doc struct {
		Origins []receipt `json:"origins"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc.Origins
}
func normalizeSource(value, workspace string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return filepath.Clean(filepath.Join(workspace, value))
	}
	value = strings.TrimPrefix(value, "https://github.com/")
	value = strings.TrimSuffix(value, ".git")
	return strings.ToLower(strings.Split(value, "#")[0])
}

// sourceMatches compares an upstream-reported source (and optional original URL) with the wanted source.
func sourceMatches(workspace, wanted, source, sourceURL string) bool {
	want := normalizeSource(wanted, workspace)
	return normalizeSource(source, workspace) == want || sourceURL != "" && normalizeSource(sourceURL, workspace) == want
}
func skillName(data []byte) (string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", fmt.Errorf("SKILL.md requires frontmatter")
	}
	header, _, ok := strings.Cut(text[4:], "\n---")
	if !ok {
		return "", fmt.Errorf("SKILL.md frontmatter is not terminated")
	}
	var meta struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(header), &meta); err != nil {
		return "", err
	}
	if meta.Name == "" {
		return "", fmt.Errorf("SKILL.md has no installation name")
	}
	return meta.Name, nil
}
func (s Service) verifiedRecord(item upstreamItem, lock projectLock) bool {
	entry, ok := lock.Skills[item.Name]
	itemURL := ""
	if item.SourceURL != nil {
		itemURL = *item.SourceURL
	}
	if !ok || entry.Hash == "" || !validSkillPath(entry.SkillPath) || item.Scope != "project" || !sourceMatches(s.Store.Workspace, item.Source, entry.Source, entry.SourceURL) && !sourceMatches(s.Store.Workspace, entry.Source, item.Source, itemURL) {
		return false
	}
	rel, err := filepath.Rel(s.Store.Workspace, item.Path)
	if err != nil || !filepath.IsLocal(rel) {
		return false
	}
	data, err := storage.Read(s.Store.Workspace, filepath.Join(rel, "SKILL.md"), 1<<20)
	if err != nil {
		return false
	}
	name, err := skillName(data)
	if err != nil || name != item.Name {
		return false
	}
	hash, err := upstreamHash(s.Store.Workspace, rel)
	return err == nil && hash == entry.Hash
}

// upstreamHash reproduces skills@1.7.6 computeSkillFolderHash, corroborating the project lock.
func upstreamHash(workspace, relative string) (string, error) {
	root := filepath.Join(workspace, relative)
	files := []string{}
	var total int64
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if e.Name() == ".git" || e.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) > 20000 {
			return fmt.Errorf("inventory file limit exceeded")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	order := collate.New(language.English)
	slices.SortFunc(files, order.CompareString)
	h := sha256.New()
	for _, name := range files {
		data, err := storage.Read(workspace, filepath.Join(relative, name), 16<<20)
		if err != nil {
			return "", err
		}
		total += int64(len(data))
		if total > 64<<20 {
			return "", fmt.Errorf("inventory byte limit exceeded")
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s Service) catalogIdentity(item upstreamItem, lock lockEntry, c domain.Catalog) (*string, *string, *string) {
	matches := []string{}
	logicalByID := map[string]string{}
	for id, skill := range c.Skills {
		if skill.Source.SkillName != item.Name {
			continue
		}
		logical := skill.Source.URI
		if skill.Source.Kind == "git" {
			if normalizeSource(logical, s.Store.Workspace) != normalizeSource(item.Source, s.Store.Workspace) {
				continue
			}
			if skill.Source.Ref != "" && skill.Source.Ref != lock.Ref {
				matched := false
				for _, receipt := range s.readReceipts() {
					if receipt.Name == item.Name && normalizeSource(receipt.LogicalSource, s.Store.Workspace) == normalizeSource(logical, s.Store.Workspace) && receipt.Ref != nil && *receipt.Ref == skill.Source.Ref && receipt.Revision != nil && *receipt.Revision == lock.Ref {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
		} else {
			logical = skill.RegistryID + ":" + filepath.ToSlash(skill.Source.Path)
			matched := false
			for _, r := range s.readReceipts() {
				if r.Name == item.Name && r.LogicalSource == logical && normalizeSource(r.Upstream, s.Store.Workspace) == normalizeSource(item.Source, s.Store.Workspace) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		matches = append(matches, id)
		logicalByID[id] = logical
	}
	if len(matches) != 1 {
		return nil, nil, nil
	}
	id := matches[0]
	logical := logicalByID[id]
	var ref *string
	if c.Skills[id].Source.Ref != "" {
		value := c.Skills[id].Source.Ref
		ref = &value
	}
	return &id, &logical, ref
}

// alreadyInstalled reports whether a verified install with the same source, revision, agents and method exists.
func (s Service) alreadyInstalled(item Item, records []upstreamItem, locked projectLock) bool {
	for _, record := range records {
		entry := locked.Skills[record.Name]
		if record.Name != item.SkillName || !agentEvidence(record.Agents, item.Agents) || entry.method() != item.Method || !s.sourceMatchesItem(record, item.Source) {
			continue
		}
		if item.Revision != nil && !filepath.IsAbs(item.Source) && entry.Ref != *item.Revision {
			continue
		}
		if s.verifiedRecord(record, locked) {
			return true
		}
	}
	return false
}

// sourceMatchesItem compares an upstream record's source or sourceUrl with a planned execution source.
func (s Service) sourceMatchesItem(record upstreamItem, source string) bool {
	url := ""
	if record.SourceURL != nil {
		url = *record.SourceURL
	}
	return sourceMatches(s.Store.Workspace, source, record.Source, url)
}

// diskInstalled corroborates known conflicts without invoking upstream during a preview.
func (s Service) diskInstalled(c domain.Catalog) map[string]bool {
	result := map[string]bool{}
	lock, err := s.readLock()
	if err != nil {
		return result
	}
	for name, entry := range lock.Skills {
		if !safeSkillName.MatchString(name) {
			continue
		}
		record := upstreamItem{Name: name, Source: entry.Source, Scope: "project", Path: filepath.Join(s.Store.Workspace, ".agents", "skills", name)}
		if !s.verifiedRecord(record, lock) {
			continue
		}
		if id, _, _ := s.catalogIdentity(record, entry, c); id != nil {
			result[*id] = true
		}
	}
	return result
}

type upstreamItem struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Path      string   `json:"path"`
	Scope     string   `json:"scope"`
	Agents    []string `json:"agents"`
	Source    string   `json:"source"`
	SourceURL *string  `json:"sourceUrl"`
	Ref       *string  `json:"ref"`
}

func (s Service) read(ctx context.Context) ([]upstreamItem, error) {
	out, err := s.run(ctx, s.Store.Workspace, s.env(), "npx", "--yes", "skills@"+Version, "list", "--json")
	if err != nil {
		return nil, err
	}
	var items []upstreamItem
	err = json.Unmarshal(out.Stdout, &items)
	return items, err
}

func agentEvidence(observed, wanted []string) bool {
	for _, id := range wanted {
		if !slices.Contains(observed, agents[id]) {
			return false
		}
	}
	return true
}

// InventoryItem is a conservative project-scoped inventory wire record.
type InventoryItem struct {
	SkillID       *string  `json:"skillId"`
	Name          string   `json:"skillName"`
	LogicalSource *string  `json:"logicalSource"`
	Ref           *string  `json:"requestedRef"`
	Agents        []string `json:"agents"`
	Scope         string   `json:"scope"`
	Status        string   `json:"status"`
	Unmanaged     bool     `json:"unmanaged"`
	Path          *string  `json:"path"`
}

// Inventory is the versioned installed-state envelope.
type Inventory struct {
	Version     int                 `json:"outputVersion"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	Workspace   string              `json:"workspace"`
	Complete    bool                `json:"complete"`
	Skills      []InventoryItem     `json:"skills"`
}

// Inventory is the versioned installed-state envelope.
func (s Service) Inventory(ctx context.Context, c domain.Catalog) Inventory {
	result := Inventory{Version: 1, Diagnostics: []domain.Diagnostic{}, Workspace: s.Store.Workspace, Skills: []InventoryItem{}}
	items, err := s.read(ctx)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_INVENTORY_UNKNOWN", Severity: "warning", Message: "Pinned upstream project inventory unavailable"})
		return result
	}
	locked, lockErr := s.readLock()
	result.Complete = lockErr == nil || len(items) == 0
	if lockErr != nil && len(items) > 0 {
		result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_INVENTORY_UNKNOWN", Severity: "warning", Message: "Project lock attribution unavailable: " + lockErr.Error() + "; fix or delete skills-lock.json"})
	}
	for _, item := range items {
		if item.Scope != "project" {
			continue
		}
		record := InventoryItem{Name: item.Name, Agents: []string{}, Scope: "project", Status: "unknown", Unmanaged: true, Path: &item.Path}
		for id, name := range agents {
			if slices.Contains(item.Agents, name) {
				record.Agents = append(record.Agents, id)
			}
		}
		slices.Sort(record.Agents)
		if lockErr == nil {
			record.SkillID, record.LogicalSource, record.Ref = s.catalogIdentity(item, locked.Skills[item.Name], c)
			record.Unmanaged = record.SkillID == nil
			if len(record.Agents) > 0 && s.verifiedRecord(item, locked) {
				record.Status = "installed"
			}
		}
		result.Skills = append(result.Skills, record)
	}
	return result
}
