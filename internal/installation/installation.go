// Package installation plans confirmed project installs and reconciles pinned upstream output.
package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Version pins the tested upstream parser and JSON inventory format.
const Version = "1.7.6"

// PolicyError marks a blocked operation that must never invoke the installer.
type PolicyError struct{ Reason string }

func (e PolicyError) Error() string { return e.Reason }

// PrerequisiteError marks a missing or incompatible installer prerequisite.
type PrerequisiteError struct{ Reason string }

func (e PrerequisiteError) Error() string { return e.Reason }

var safeSkillName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Service coordinates source resolution, approval and project installation.
type Service struct {
	Store    settings.Store
	Settings settings.Settings
	Runner   process.Executor
}

// Item records one selected source and its stable logical origin.
type Item struct {
	SkillID       *string  `json:"skillId"`
	SkillName     string   `json:"skillName"`
	Source        string   `json:"source"`
	LogicalSource string   `json:"logicalSource"`
	RegistryID    *string  `json:"sourceRegistryId"`
	Ref           *string  `json:"ref"`
	Revision      *string  `json:"resolvedRevision"`
	RepositoryID  *string  `json:"repositoryId"`
	Provenance    string   `json:"provenance"`
	Agents        []string `json:"agents"`
	Method        string   `json:"method"`
	Status        string   `json:"status"`
}

// Command contains validated argv and aligned per-skill identities.
type Command struct {
	Argv          []string  `json:"argv"`
	Source        string    `json:"source"`
	LogicalSource string    `json:"logicalSource"`
	Ref           *string   `json:"ref"`
	Revision      *string   `json:"resolvedRevision"`
	SkillIDs      []*string `json:"skillIds"`
	SkillNames    []string  `json:"skillNames"`
	Agents        []string  `json:"agents"`
	Method        string    `json:"method"`
}

// Override explains a source/provenance change requiring review.
type Override struct {
	SkillID *string `json:"skillId"`
	Field   string  `json:"field"`
	From    *string `json:"fromRegistryId"`
	To      string  `json:"toRegistryId"`
	Reason  string  `json:"reason"`
}

// Conflict records a selected or installed relationship blocker.
type Conflict struct {
	SkillID *string `json:"skillId"`
	Other   string  `json:"conflictingSkillId"`
	Kind    string  `json:"kind"`
	Ignored bool    `json:"ignored"`
	Reason  string  `json:"reason"`
}

// Fingerprint binds an installation source to immutable content.
type Fingerprint struct {
	LogicalSource string  `json:"logicalSource"`
	RegistryID    *string `json:"sourceRegistryId"`
	Ref           *string `json:"requestedRef"`
	Revision      *string `json:"resolvedRevision"`
	Hash          string  `json:"contentSha256"`
}

// RegistryFingerprint binds review to a registry snapshot/content generation.
type RegistryFingerprint struct {
	ID       string  `json:"registryId"`
	Revision *string `json:"resolvedRevision"`
	Hash     string  `json:"contentSha256"`
}

// Approval carries review requirements and the canonical plan digest.
type Approval struct {
	Required         bool                  `json:"required"`
	Digest           *string               `json:"digest"`
	Canonicalization string                `json:"canonicalization"`
	Version          string                `json:"installerVersion"`
	Sources          []Fingerprint         `json:"sourceFingerprints"`
	Registries       []RegistryFingerprint `json:"registryFingerprints"`
}

// Plan contains the immutable reviewed project installation commands.
type Plan struct {
	Version       int                 `json:"outputVersion"`
	Diagnostics   []domain.Diagnostic `json:"diagnostics"`
	Blocked       bool                `json:"blocked"`
	Workspace     string              `json:"workspace"`
	Agents        []string            `json:"agents"`
	Method        string              `json:"method"`
	Commands      []Command           `json:"commands"`
	Items         []Item              `json:"items"`
	Overrides     []Override          `json:"overrides"`
	Conflicts     []Conflict          `json:"conflicts"`
	Approval      Approval            `json:"approval"`
	trees         map[string]string
	registryRoots map[string]string
}

// Options selects catalog or direct-source installation targets.
type Options struct {
	IDs             []string
	Agents          []string
	Method          string
	Source          string
	SkillName       string
	IgnoreConflicts bool
}

func (s Service) env() []string {
	if s.Settings.Installation.Telemetry == "disabled" {
		return []string{"DISABLE_TELEMETRY=1", "DO_NOT_TRACK=1"}
	}
	return nil
}

// Plan contains the immutable reviewed project installation commands.
func (s Service) Plan(ctx context.Context, c domain.Catalog, recs catalog.Recommendations, opts Options) (Plan, error) {
	selected := slices.Clone(opts.IDs)
	selected = unique(selected)
	chosen := unique(opts.Agents)
	if len(chosen) == 0 {
		chosen = unique(s.Settings.Installation.Agents)
	}
	if len(chosen) == 0 {
		return Plan{}, fmt.Errorf("explicit --agent or configured installation.agents required")
	}
	for _, id := range chosen {
		if _, ok := agents[id]; !ok {
			return Plan{}, PrerequisiteError{Reason: "unsupported agent " + id}
		}
	}
	method := opts.Method
	if method == "" {
		method = s.Settings.Installation.Method
	}
	if method != "copy" && method != "symlink" {
		return Plan{}, fmt.Errorf("invalid installation method")
	}
	p := Plan{Version: 3, Diagnostics: []domain.Diagnostic{}, Workspace: s.Store.Workspace, Agents: chosen, Method: method, Commands: []Command{}, Items: []Item{}, Overrides: []Override{}, Conflicts: []Conflict{}, Approval: Approval{Canonicalization: "nosey-plan-v1", Version: Version, Sources: []Fingerprint{}, Registries: []RegistryFingerprint{}}, trees: map[string]string{}, registryRoots: c.Roots}
	if s.Settings.Installation.Version != Version {
		return p, PrerequisiteError{Reason: "only skills@" + Version + " is supported"}
	}
	visit := map[string]int{}
	ordered := []string{}
	var walk func(string) error
	walk = func(id string) error {
		if visit[id] == 1 {
			return fmt.Errorf("requirement cycle involving %s", id)
		}
		if visit[id] == 2 {
			return nil
		}
		skill, ok := c.Skills[id]
		if !ok {
			return fmt.Errorf("unknown required skill %s", id)
		}
		visit[id] = 1
		requirements := unique(append(slices.Clone(skill.Requires), recs.Requires[id]...))
		for _, dep := range requirements {
			if dep == id {
				return fmt.Errorf("self requirement %s", id)
			}
			if err := walk(dep); err != nil {
				return err
			}
		}
		visit[id] = 2
		ordered = append(ordered, id)
		return nil
	}
	for _, id := range selected {
		if err := walk(id); err != nil {
			return p, err
		}
	}
	if opts.Source != "" {
		if len(selected) > 0 || opts.SkillName == "" {
			return p, fmt.Errorf("direct source requires --skill and no catalog IDs")
		}
		ordered = []string{""}
	}
	if len(ordered) == 0 {
		return p, fmt.Errorf("select at least one skill")
	}
	installed := s.diskInstalled(c)
	for _, id := range ordered {
		skill := c.Skills[id]
		var sid, rid, repositoryID *string
		provenance := "catalogued"
		if id != "" {
			sid = nullable(id)
			rid = nullable(skill.RegistryID)
			repositoryID = nullable(skill.RepositoryID)
		} else {
			skill = domain.Skill{Name: opts.SkillName, Source: domain.Source{Kind: "git", URI: opts.Source, SkillName: opts.SkillName}}
			if !filepath.IsAbs(opts.Source) {
				if uri, requested, ok := strings.Cut(opts.Source, "#"); ok {
					skill.Source.URI = uri
					skill.Source.Ref = requested
				}
			}
			if filepath.IsAbs(opts.Source) {
				skill.Source.Kind = "local"
				skill.Source.Path = opts.Source
			}
			provenance = "uncatalogued"
		}
		if !safeSkillName.MatchString(skill.Source.SkillName) {
			return p, fmt.Errorf("unsafe upstream skill name %q", skill.Source.SkillName)
		}
		var ref, revision *string
		logical := skill.Source.URI
		var actual string
		tree := ""
		hash := ""
		if skill.Source.Ref != "" {
			v := skill.Source.Ref
			ref = &v
		}
		if skill.Source.Kind == "local" {
			root := c.Roots[skill.RegistryID]
			if id == "" {
				root = filepath.Dir(skill.Source.Path)
				skill.Source.Path = filepath.Base(skill.Source.Path)
			}
			if !filepath.IsLocal(skill.Source.Path) {
				return p, fmt.Errorf("local skill path must remain inside registry")
			}
			tree = filepath.Join(root, skill.Source.Path)
			data, err := storage.Read(root, filepath.Join(skill.Source.Path, "SKILL.md"), 1<<20)
			if err != nil {
				return p, err
			}
			parsed, err := skillName(data)
			if err != nil || parsed != skill.Source.SkillName {
				return p, fmt.Errorf("local source frontmatter name does not match %s", skill.Source.SkillName)
			}
			logical = skill.RegistryID + ":" + filepath.ToSlash(skill.Source.Path)
			if id == "" {
				logical = filepath.Clean(opts.Source)
			}
			hash, err = treeHash(tree)
			if err != nil {
				return p, err
			}
			actual = filepath.Join(s.Store.Data, "install-sources", hash)
			p.trees[actual] = tree
			revision = c.Revisions[skill.RegistryID]
		} else {
			if err := registry.ValidateGit(logical, skill.Source.Ref); err != nil {
				return p, err
			}
			root, sha, err := s.gitSource(ctx, logical, skill.Source.Ref)
			if err != nil {
				return p, err
			}
			tree = root
			revision = &sha
			hash, err = treeHash(root)
			if err != nil {
				return p, err
			}
			actual = logical + "#" + sha
		}
		item := Item{sid, skill.Source.SkillName, actual, logical, rid, ref, revision, repositoryID, provenance, chosen, method, "planned"}
		argv := []string{"npx", "--yes", "skills@" + Version, "add", actual, "--full-depth", "--skill", skill.Source.SkillName, "--agent"}
		argv = append(argv, chosen...)
		argv = append(argv, "--json", "--yes")
		if method == "copy" {
			argv = append(argv, "--copy")
		}
		p.Items = append(p.Items, item)
		p.Commands = append(p.Commands, Command{argv, actual, logical, ref, revision, []*string{sid}, []string{skill.Source.SkillName}, chosen, method})
		p.Approval.Sources = append(p.Approval.Sources, Fingerprint{logical, rid, ref, revision, hash})
		if id != "" {
			for _, conflict := range unique(append(slices.Clone(skill.ConflictsWith), recs.Conflicts[id]...)) {
				if visit[conflict] > 0 || c.InstallStatus[conflict] == "installed" || installed[conflict] {
					kind, reason := "selected", "Selected skills conflict"
					if visit[conflict] == 0 {
						kind, reason = "installed", "Selected skill conflicts with an installed skill"
					}
					p.Conflicts = append(p.Conflicts, Conflict{sid, conflict, kind, opts.IgnoreConflicts, reason})
					if !opts.IgnoreConflicts {
						p.Blocked = true
					}
				}
			}
			if skill.RegistryID != "nosey:official" {
				p.Approval.Required = true
				p.Overrides = append(p.Overrides, Override{sid, "source", nil, skill.RegistryID, "Custom registry installation source requires review"})
			}
		}
		if skill.Source.Kind == "git" && (ref == nil || !regexp.MustCompile(`^[a-fA-F0-9]{40}$`).MatchString(*ref)) {
			p.Approval.Required = true
		}
	}
	registryIDs := []string{}
	for id := range c.Roots {
		registryIDs = append(registryIDs, id)
	}
	slices.Sort(registryIDs)
	for _, id := range registryIDs {
		hash, err := treeHash(c.Roots[id])
		if err != nil {
			return p, err
		}
		p.Approval.Registries = append(p.Approval.Registries, RegistryFingerprint{id, c.Revisions[id], hash})
	}
	if !p.Blocked {
		digest, err := Digest(p)
		if err != nil {
			return p, err
		}
		p.Approval.Digest = &digest
	}
	return p, nil
}

// Digest hashes the approved semantic projection, excluding presentation/consent state.
func Digest(p Plan) (string, error) {
	bytes, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(bytes, &doc); err != nil {
		return "", err
	}
	delete(doc, "diagnostics")
	delete(doc, "blocked")
	var approval map[string]json.RawMessage
	if err = json.Unmarshal(doc["approval"], &approval); err != nil {
		return "", err
	}
	delete(approval, "digest")
	doc["approval"], err = json.Marshal(approval)
	if err != nil {
		return "", err
	}
	var items []map[string]json.RawMessage
	if err = json.Unmarshal(doc["items"], &items); err != nil {
		return "", err
	}
	for _, item := range items {
		delete(item, "status")
	}
	doc["items"], err = json.Marshal(items)
	if err != nil {
		return "", err
	}
	var value any
	if err = json.Unmarshal(mustJSON(doc), &value); err != nil {
		return "", err
	}
	var b strings.Builder
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(value); err != nil {
		return "", err
	}
	return storage.Hash([]byte(strings.TrimSuffix(b.String(), "\n"))), nil
}

// mustJSON handles only an already decoded JSON object; Marshal cannot fail for RawMessage values validated above.
func mustJSON(value map[string]json.RawMessage) []byte { data, _ := json.Marshal(value); return data }
func (s Service) gitSource(ctx context.Context, uri, ref string) (string, string, error) {
	key := storage.Hash([]byte(uri + "#" + ref))
	dir := filepath.Join(s.Store.Cache, "skill-sources", key)
	lock, err := storage.Acquire(ctx, dir+".lock")
	if err != nil {
		return "", "", err
	}
	defer lock.Close()
	root := filepath.Join(dir, "checkout")
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return "", "", err
	}
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:ssh", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_LFS_SKIP_SMUDGE=1"}
	remote := uri
	if !strings.Contains(remote, ":") && !strings.HasPrefix(remote, "git@") {
		remote = "https://github.com/" + remote
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, os.ErrNotExist) {
		_, err = s.run(ctx, "", env, "git", "-c", "core.hooksPath="+os.DevNull, "clone", "--no-checkout", "--depth=1", remote, root)
		if err != nil {
			return "", "", fmt.Errorf("skill source unavailable: %w", err)
		}
	}
	if ref == "" {
		ref = "HEAD"
	}
	_, err = s.run(ctx, root, env, "git", "-c", "core.hooksPath="+os.DevNull, "fetch", "--depth=1", "origin", ref)
	if err != nil {
		return "", "", err
	}
	_, err = s.run(ctx, root, env, "git", "-c", "core.hooksPath="+os.DevNull, "-c", "filter.lfs.smudge=", "-c", "filter.lfs.required=false", "checkout", "--detach", "FETCH_HEAD")
	if err != nil {
		return "", "", err
	}
	out, err := s.run(ctx, root, env, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	sha := strings.TrimSpace(string(out.Stdout))
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(sha) {
		return "", "", fmt.Errorf("invalid resolved Git revision")
	}
	snapshot := filepath.Join(dir, sha)
	if _, err = os.Stat(snapshot); errors.Is(err, os.ErrNotExist) {
		if err = copyTree(root, snapshot); err != nil {
			return "", "", err
		}
	} else if err != nil {
		return "", "", err
	}
	fresh, err := treeHash(root)
	if err != nil {
		return "", "", err
	}
	saved, err := treeHash(snapshot)
	if err != nil || fresh != saved {
		return "", "", fmt.Errorf("skill source snapshot integrity failure")
	}
	return snapshot, sha, nil
}
func treeHash(root string) (string, error) {
	var b strings.Builder
	count := 0
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source symlinks are not supported")
		}
		count++
		if count > 20000 {
			return fmt.Errorf("source file-count limit exceeded")
		}
		data, err := storage.Read(root, rel, 16<<20)
		if err != nil {
			return err
		}
		total += int64(len(data))
		if total > 64<<20 {
			return fmt.Errorf("source byte limit exceeded")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s\x00%d\x00%s\n", filepath.ToSlash(rel), info.Mode().Perm()&0o111, storage.Hash(data))
		return nil
	})
	return storage.Hash([]byte(b.String())), err
}

// Execute enforces reviewed consent and verifies individual project install outcomes.
func (s Service) Execute(ctx context.Context, p Plan, approval string, confirmed bool) error {
	if !confirmed {
		return PolicyError{Reason: "explicit confirmation required"}
	}
	if p.Blocked || p.Approval.Digest == nil {
		return PolicyError{Reason: "installation plan is blocked"}
	}
	digest, err := Digest(p)
	if err != nil {
		return err
	}
	if p.Approval.Required && approval != digest {
		return PolicyError{Reason: "additional approval requires --approve-plan " + digest + " with --yes"}
	}
	projectRoot, err := os.OpenRoot(s.Store.Workspace)
	if err != nil {
		return err
	}
	defer func() { _ = projectRoot.Close() }()
	if err = projectRoot.MkdirAll(".nosey", 0o700); err != nil {
		return err
	}
	lock, err := projectRoot.OpenFile(".nosey/operation.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("another project operation is active or its lock path is unsafe: %w", err)
	}
	_ = lock.Close()
	defer func() { _ = projectRoot.Remove(".nosey/operation.lock") }()
	for _, f := range p.Approval.Registries {
		root, ok := p.registryRoots[f.ID]
		if !ok {
			continue
		}
		hash, err := treeHash(root)
		if err != nil || hash != f.Hash {
			return PolicyError{Reason: "registry content changed since review"}
		}
	}
	for dst, src := range p.trees {
		expected := filepath.Base(dst)
		actual, hashErr := treeHash(src)
		if hashErr != nil || actual != expected {
			return fmt.Errorf("reviewed source changed")
		}
		if _, statErr := os.Stat(dst); errors.Is(statErr, os.ErrNotExist) {
			sourceLock, lockErr := storage.Acquire(ctx, dst+".lock")
			if lockErr != nil {
				return lockErr
			}
			if _, statErr = os.Stat(dst); errors.Is(statErr, os.ErrNotExist) {
				err = copyTree(src, dst)
			}
			sourceLock.Close()
			if err != nil {
				return err
			}
		}
		actual, hashErr = treeHash(dst)
		if hashErr != nil || actual != expected {
			return fmt.Errorf("persistent installation source integrity failure")
		}
	}
	paths := []string{".agents/skills", "skills-lock.json"}
	for _, item := range p.Items {
		for _, agent := range item.Agents {
			paths = append(paths, agentPaths[agent], filepath.Join(agentPaths[agent], item.SkillName))
		}
	}
	for _, path := range unique(paths) {
		if _, err = projectRoot.Stat(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PolicyError{Reason: "installation path escapes project or is inaccessible: " + path}
		}
	}
	if err = s.requirePrerequisites(ctx); err != nil {
		return err
	}
	for i, cmd := range p.Commands {
		if s.alreadyInstalled(ctx, p.Items[i]) {
			if err = s.receipt(p.Items[i]); err != nil {
				return err
			}
			continue
		}
		out, runErr := s.run(ctx, p.Workspace, s.env(), cmd.Argv[0], cmd.Argv[1:]...)
		if runErr != nil {
			return fmt.Errorf("installation failed for %s: %w", p.Items[i].SkillName, runErr)
		}
		var installed []upstreamItem
		if err = json.Unmarshal(out.Stdout, &installed); err != nil {
			return fmt.Errorf("invalid upstream installation result: %w", err)
		}
		item := p.Items[i]
		matched := false
		for _, record := range installed {
			if record.Name == item.SkillName && record.Status == "installed" && record.Scope == "project" && agentEvidence(record.Agents, item.Agents) && normalizeSource(record.Source, p.Workspace) == normalizeSource(item.Source, p.Workspace) && (item.Revision == nil || record.Ref != nil && *record.Ref == *item.Revision || filepath.IsAbs(item.Source)) {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("per-skill installation success not established: %s", item.SkillName)
		}
		inventory, readErr := s.read(ctx)
		if readErr != nil {
			return readErr
		}
		verified := false
		locked, lockErr := s.readLock()
		if lockErr != nil {
			return lockErr
		}
		for _, record := range inventory {
			if record.Name == item.SkillName && record.Scope == "project" && agentEvidence(record.Agents, item.Agents) && s.verifiedRecord(record, locked) && normalizeSource(record.Source, p.Workspace) == normalizeSource(item.Source, p.Workspace) {
				verified = true
			}
		}
		if !verified {
			return fmt.Errorf("post-install verification unknown: %s", item.SkillName)
		}
		if err = s.receipt(item); err != nil {
			return err
		}
	}
	return nil
}
func copyTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".install-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	err = filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(stage, rel), 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe source symlink")
		}
		data, err := storage.Read(src, rel, 16<<20)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(stage, rel), data, 0o600|info.Mode().Perm()&0o111)
	})
	if err != nil {
		return err
	}
	return os.Rename(stage, dst)
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
		result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{Code: "W_INVENTORY_UNKNOWN", Severity: "warning", Message: "Project lock attribution is unavailable or malformed"})
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

type receipt struct {
	SkillID       *string  `json:"skillId"`
	Name          string   `json:"frontmatterName"`
	LogicalSource string   `json:"logicalSource"`
	RegistryID    *string  `json:"sourceRegistryId"`
	Path          *string  `json:"relativeSkillPath"`
	Ref           *string  `json:"requestedRef"`
	Revision      *string  `json:"resolvedRevision"`
	Agents        []string `json:"agents"`
	Method        string   `json:"method"`
	Provenance    string   `json:"provenance"`
	Upstream      string   `json:"upstreamSourceAtInstall"`
	At            string   `json:"installedAt"`
}

func (s Service) receipt(item Item) error {
	path := ".nosey/install-origins.json"
	doc := struct {
		Version int       `json:"schemaVersion"`
		Origins []receipt `json:"origins"`
	}{1, []receipt{}}
	// #nosec G304 -- this path is derived from the canonical workspace or application-owned cache.
	data, err := storage.Read(s.Store.Workspace, path, 4<<20)
	if err == nil {
		if err = json.Unmarshal(data, &doc); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r := receipt{item.SkillID, item.SkillName, item.LogicalSource, item.RegistryID, nil, item.Ref, item.Revision, item.Agents, item.Method, item.Provenance, item.Source, time.Now().UTC().Format(time.RFC3339)}
	idx := slices.IndexFunc(doc.Origins, func(v receipt) bool { return v.LogicalSource == r.LogicalSource && v.Name == r.Name })
	if idx < 0 {
		doc.Origins = append(doc.Origins, r)
	} else {
		doc.Origins[idx] = r
	}
	bytes, err := contracts.Encode("install-origins.schema.json", doc)
	if err != nil {
		return err
	}
	return storage.WriteAt(s.Store.Workspace, path, bytes)
}
func unique(v []string) []string { v = slices.Clone(v); slices.Sort(v); return slices.Compact(v) }

// CommandText escapes argv for display/copy; execution always uses tokens directly.
func CommandText(argv []string) string {
	parts := []string{}
	for _, arg := range argv {
		parts = append(parts, "\""+strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`").Replace(arg)+"\"")
	}
	return strings.Join(parts, " ")
}

func (s Service) run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	runner := s.Runner
	if runner == nil {
		runner = process.Runner{}
	}
	return runner.Run(ctx, dir, env, name, args...)
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// CommandsReady prevents copying raw local-source commands before explicit execution has prepared them.
func (p Plan) CommandsReady() error {
	if p.Blocked {
		return PolicyError{Reason: "installation plan is blocked; resolve its conflicts before copying commands"}
	}
	for path := range p.trees {
		hash, err := treeHash(path)
		if err != nil || hash != filepath.Base(path) {
			return PolicyError{Reason: "local sources are not prepared; execute the reviewed plan through nosey before copying raw upstream commands"}
		}
	}
	return nil
}
