package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

type lockEntry struct {
	Source string `json:"source"`
	Type   string `json:"sourceType"`
	Ref    string `json:"ref"`
	Hash   string `json:"computedHash"`
}
type projectLock struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

func (s Service) readLock() (projectLock, error) {
	var lock projectLock
	data, err := storage.Read(s.Store.Workspace, "skills-lock.json", 16<<20)
	if err != nil {
		return lock, err
	}
	if err = json.Unmarshal(data, &lock); err != nil {
		return lock, err
	}
	if lock.Version != 1 || lock.Skills == nil {
		return lock, fmt.Errorf("unsupported upstream project lock format")
	}
	return lock, nil
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
	if !ok || entry.Hash == "" || item.Scope != "project" || normalizeSource(entry.Source, s.Store.Workspace) != normalizeSource(item.Source, s.Store.Workspace) {
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

func (s Service) alreadyInstalled(ctx context.Context, item Item) bool {
	records, err := s.read(ctx)
	if err != nil {
		return false
	}
	locked, err := s.readLock()
	if err != nil {
		return false
	}
	for _, record := range records {
		entry := locked.Skills[record.Name]
		if record.Name != item.SkillName || !agentEvidence(record.Agents, item.Agents) || normalizeSource(record.Source, s.Store.Workspace) != normalizeSource(item.Source, s.Store.Workspace) {
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
