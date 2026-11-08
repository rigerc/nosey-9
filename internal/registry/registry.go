// Package registry resolves data-only sources and compiles provenance-aware overlays.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Snapshot identifies one validated data source and immutable Git revision.
type Snapshot struct {
	Root     string
	Revision *string
	Manifest domain.Manifest
	Stale    bool
}

// Service fetches sources and compiles effective registry entities.
type Service struct {
	Store  settings.Store
	Runner process.Executor
}
type lock struct{ path string }

func acquire(ctx context.Context, path string) (lock, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return lock{}, err
	}
	for {
		// #nosec G304 -- this path is derived from the canonical workspace or application-owned cache.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return lock{path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return lock{}, err
		}
		select {
		case <-ctx.Done():
			return lock{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (l lock) close() { _ = os.Remove(l.path) }

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var gitURI = regexp.MustCompile(`^(https://[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+|ssh://([A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+|git@[A-Za-z0-9.-]+:[A-Za-z0-9._/-]+|[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+)$`)

// ValidateGit rejects unsupported URI/ref forms before argv construction.
func ValidateGit(uri, ref string) error {
	if !gitURI.MatchString(uri) || strings.Contains(uri, "..") || strings.HasPrefix(uri, "-") {
		return fmt.Errorf("unsafe Git URI")
	}
	if ref != "" && (!refPattern.MatchString(ref) || strings.Contains(ref, "..") || strings.HasSuffix(ref, "/") || strings.Contains(ref, "//")) {
		return fmt.Errorf("unsafe Git ref")
	}
	return nil
}

// Resolve loads local data or promotes a validated immutable Git snapshot.
func (s Service) Resolve(ctx context.Context, ref domain.RegistryRef, ttl int, refresh bool) (Snapshot, error) {
	if ref.Source.Kind == "local" {
		root, err := filepath.EvalSymlinks(ref.Source.Path)
		if err != nil {
			return Snapshot{}, err
		}
		m, err := readManifest(root)
		return Snapshot{Root: root, Manifest: m}, err
	}
	if err := ValidateGit(ref.Source.URI, ref.Source.Ref); err != nil {
		return Snapshot{}, err
	}
	if ref.TTLHours > 0 {
		ttl = ref.TTLHours
	}
	key := storage.Hash([]byte(ref.Source.URI + "#" + ref.Source.Ref))
	dir := filepath.Join(s.Store.Cache, "registries", key)
	pointer := filepath.Join(dir, "current.json")
	var saved struct {
		Revision string    `json:"revision"`
		Fetched  time.Time `json:"fetched"`
	}
	// #nosec G304 -- this path is derived from the canonical workspace or application-owned cache.
	data, err := os.ReadFile(pointer)
	if err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	previous := func() (Snapshot, error) {
		if saved.Revision == "" {
			return Snapshot{}, fmt.Errorf("registry unavailable: %s", ref.ID)
		}
		root := filepath.Join(dir, saved.Revision)
		m, readErr := readManifest(root)
		sha := saved.Revision
		return Snapshot{Root: root, Revision: &sha, Manifest: m, Stale: true}, readErr
	}
	if saved.Revision != "" && !refresh && time.Since(saved.Fetched) < time.Duration(ttl)*time.Hour {
		snap, readErr := previous()
		snap.Stale = false
		return snap, readErr
	}
	l, err := acquire(ctx, dir+".lock")
	if err != nil {
		return Snapshot{}, err
	}
	defer l.close()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return Snapshot{}, err
	}
	stage, err := os.MkdirTemp(dir, "fetch-")
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	uri := ref.Source.URI
	if !strings.Contains(uri, ":") && !strings.HasPrefix(uri, "git@") {
		uri = "https://github.com/" + uri
	}
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:ssh", "GIT_LFS_SKIP_SMUDGE=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	args := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "protocol.ext.allow=never", "-c", "submodule.recurse=false", "clone", "--no-checkout", "--depth=1", uri, stage}
	_, err = s.run(ctx, "", env, "git", args...)
	if err == nil {
		requested := ref.Source.Ref
		if requested == "" {
			requested = "HEAD"
		}
		_, err = s.run(ctx, stage, env, "git", "-c", "core.hooksPath="+os.DevNull, "fetch", "--depth=1", "origin", requested)
	}
	if err == nil {
		_, err = s.run(ctx, stage, env, "git", "-c", "core.hooksPath="+os.DevNull, "-c", "filter.lfs.smudge=", "-c", "filter.lfs.required=false", "checkout", "--detach", "FETCH_HEAD")
	}
	var revision string
	if err == nil {
		out, runErr := s.run(ctx, stage, env, "git", "rev-parse", "HEAD")
		err = runErr
		revision = strings.TrimSpace(string(out.Stdout))
	}
	if err == nil {
		err = validateSource(stage)
	}
	if err != nil {
		snap, oldErr := previous()
		if oldErr == nil {
			return snap, nil
		}
		return Snapshot{}, fmt.Errorf("registry %s refresh failed: %w", ref.ID, err)
	}
	destination := filepath.Join(dir, revision)
	if _, statErr := os.Stat(destination); errors.Is(statErr, os.ErrNotExist) {
		if err = os.Rename(stage, destination); err != nil {
			return Snapshot{}, err
		}
	}
	saved.Revision = revision
	saved.Fetched = time.Now()
	if err = storage.JSON(pointer, saved); err != nil {
		return Snapshot{}, err
	}
	m, err := readManifest(destination)
	return Snapshot{Root: destination, Revision: &revision, Manifest: m}, err
}
func readManifest(root string) (domain.Manifest, error) {
	var manifest domain.Manifest
	data, err := storage.Read(root, "nosey.registry.json", 1<<20)
	if err != nil {
		return manifest, err
	}
	if err = contracts.Validate("registry.schema.json", data); err != nil {
		return manifest, err
	}
	err = json.Unmarshal(data, &manifest)
	return manifest, err
}
func validateSource(root string) error {
	m, err := readManifest(root)
	if err != nil {
		return err
	}
	for kind, name := range m.Files {
		data, readErr := storage.Read(root, name, 16<<20)
		if readErr != nil {
			return readErr
		}
		schema := kind + ".schema.json"
		if kind == "technologies" {
			var header struct {
				Version int `json:"schemaVersion"`
			}
			if err = json.Unmarshal(data, &header); err != nil {
				return err
			}
			if header.Version == 2 {
				schema = "technologies.v2.schema.json"
			} else if header.Version != 1 {
				return fmt.Errorf("unsupported technology version %d", header.Version)
			}
		}
		if err = contracts.Validate(schema, data); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// Load merges enabled sources while retaining field provenance and invalidation.
func (s Service) Load(ctx context.Context, resolved settings.Resolved, refresh bool) (domain.Catalog, error) {
	c := domain.Catalog{Raw: map[string]map[string]json.RawMessage{}, History: map[string]map[string][]string{}, Sources: map[string]domain.RegistrySource{}, ProjectSources: resolved.WorkspaceIDs, Technologies: map[string]domain.Technology{}, Skills: map[string]domain.Skill{}, Mappings: map[string]domain.Mapping{}, Repositories: map[string]domain.Repository{}, Scores: map[string]domain.RepositoryScore{}, Provenance: map[string]map[string]string{}, Roots: map[string]string{}, Revisions: map[string]*string{}, InvalidTechnologies: map[string]string{}, Diagnostics: append([]domain.Diagnostic{}, resolved.Diagnostics...)}
	refs := slices.Clone(resolved.Settings.Registries)
	slices.SortStableFunc(refs, func(a, b domain.RegistryRef) int {
		if a.Priority != b.Priority {
			return a.Priority - b.Priority
		}
		if a.ID == "nosey:official" {
			return -1
		}
		if b.ID == "nosey:official" {
			return 1
		}
		return 0
	})
	collections := map[string]map[string]domain.RawEntity{"technologies": {}, "skills": {}, "mappings": {}, "repositories": {}}
	invalid := map[string]string{}
	for _, ref := range refs {
		if !ref.Enabled {
			continue
		}
		snap, err := s.Resolve(ctx, ref, resolved.Settings.Updates.TTL, refresh)
		if err != nil {
			if ref.ID == "nosey:official" {
				return c, err
			}
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_REGISTRY_UNAVAILABLE", Severity: "warning", Message: storage.Safe(err.Error()), Subject: ref.ID})
			continue
		}
		if err = validateSource(snap.Root); err != nil {
			return c, err
		}
		c.Roots[ref.ID] = snap.Root
		c.Sources[ref.ID] = ref.Source
		c.Revisions[ref.ID] = snap.Revision
		if snap.Stale {
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_STALE_CACHE", Severity: "warning", Message: "Using last validated snapshot", Subject: ref.ID})
		}
		for id, score := range snap.Manifest.Scores {
			countProvided := score.Count != nil
			if previous, ok := c.Scores[id]; ok && score.Count == nil {
				score.Count = previous.Count
			}
			c.Scores[id] = score
			key := "repositoryScores:" + id
			if c.Provenance[key] == nil {
				c.Provenance[key] = map[string]string{}
			}
			c.Provenance[key]["averageScore"] = ref.ID
			if countProvided {
				c.Provenance[key]["scoreCount"] = ref.ID
			}
		}
		for kind, name := range snap.Manifest.Files {
			data, readErr := storage.Read(snap.Root, name, 16<<20)
			if readErr != nil {
				return c, readErr
			}
			var doc map[string]json.RawMessage
			if err = json.Unmarshal(data, &doc); err != nil {
				return c, err
			}
			var entities []domain.RawEntity
			if err = json.Unmarshal(doc[kind], &entities); err != nil {
				return c, err
			}
			seen := map[string]bool{}
			if kind == "skills" {
				sums := map[string]float64{}
				counts := map[string]int{}
				for _, entity := range entities {
					var repo string
					var score *float64
					var disabled bool
					_ = json.Unmarshal(entity["repositoryId"], &repo)
					_ = json.Unmarshal(entity["score"], &score)
					_ = json.Unmarshal(entity["disabled"], &disabled)
					if repo != "" && score != nil && !disabled {
						sums[repo] += *score
						counts[repo]++
					}
				}
				for id, score := range snap.Manifest.Scores {
					if counts[id] > 0 && math.Abs(score.Average-sums[id]/float64(counts[id])) > 0.5 {
						c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "W_SCORE_DRIFT", Severity: "warning", Message: "Authored repository average differs from this source's scored-skill mean", Subject: ref.ID + ":" + id})
					}
				}
			}
			for _, entity := range entities {
				var id string
				if err = json.Unmarshal(entity["id"], &id); err != nil {
					return c, err
				}
				if seen[id] {
					return c, fmt.Errorf("duplicate %s ID %s", kind, id)
				}
				seen[id] = true
				key := kind + ":" + id
				if c.Provenance[key] == nil {
					c.Provenance[key] = map[string]string{}
				}
				base := collections[kind][id]
				if base == nil {
					base = domain.RawEntity{}
				}
				if err = mergeEntity(base, entity, kind, c.Provenance[key], ref.ID); err != nil {
					invalid[key] = err.Error()
				}
				collections[kind][id] = base
				if c.History[key] == nil {
					c.History[key] = map[string][]string{}
				}
				for field, origin := range c.Provenance[key] {
					history := c.History[key][field]
					if len(history) == 0 || history[len(history)-1] != origin {
						c.History[key][field] = append(history, origin)
					}
				}
			}
		}
	}
	for kind, entities := range collections {
		for id, entity := range entities {
			key := kind + ":" + id
			var disabled bool
			_ = json.Unmarshal(entity["disabled"], &disabled)
			if disabled {
				continue
			}
			data, _ := json.Marshal(entity)
			reason := invalid[key]
			switch kind {
			case "skills":
				var skill domain.Skill
				if err := json.Unmarshal(data, &skill); err != nil {
					return c, err
				}
				check, _ := json.Marshal(struct {
					Version int                `json:"schemaVersion"`
					Skills  []domain.RawEntity `json:"skills"`
				}{2, []domain.RawEntity{entity}})
				if err := contracts.Validate("skills.effective.schema.json", check); err != nil {
					reason = err.Error()
				}
				if reason == "" {
					skill.RegistryID = c.Provenance[key]["source"]
					skill.Name = storage.Safe(skill.Name)
					skill.Description = storage.Safe(skill.Description)
					for i := range skill.Tags {
						skill.Tags[i] = storage.Safe(skill.Tags[i])
					}
					c.Skills[id] = skill
				}
			case "mappings":
				var mapping domain.Mapping
				_ = json.Unmarshal(data, &mapping)
				check, _ := json.Marshal(struct {
					Version  int                `json:"schemaVersion"`
					Mappings []domain.RawEntity `json:"mappings"`
				}{2, []domain.RawEntity{entity}})
				if err := contracts.Validate("mappings.effective.schema.json", check); err != nil {
					reason = err.Error()
				}
				if reason == "" {
					c.Mappings[id] = mapping
				}
			case "technologies":
				var technology domain.Technology
				_ = json.Unmarshal(data, &technology)
				if technology.Name == "" || technology.Category == "" || (!technology.MetadataOnly && len(technology.Rules) == 0) {
					reason = "incomplete effective technology"
				}
				if reason == "" {
					technology.Name = storage.Safe(technology.Name)
					c.Technologies[id] = technology
				} else {
					c.InvalidTechnologies[id] = reason
				}
			case "repositories":
				var repo domain.Repository
				_ = json.Unmarshal(data, &repo)
				if repo.Name == "" || repo.Kind == "" {
					reason = "incomplete repository"
				}
				if reason == "" {
					repo.Name = storage.Safe(repo.Name)
					c.Repositories[id] = repo
				}
			}
			if reason != "" {
				c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "E_EFFECTIVE_ENTITY", Severity: "error", Message: storage.Safe(reason), Subject: key})
			}
		}
	}
	semanticSkills(&c)
	for id, m := range c.Mappings {
		valid := true
		if _, ok := c.Technologies[m.TechnologyID]; !ok {
			valid = false
		}
		for _, r := range m.Recommendations {
			if _, ok := c.Skills[r.SkillID]; !ok {
				valid = false
			}
			for _, required := range append(slices.Clone(r.Requires), r.ConflictsWith...) {
				if _, ok := c.Skills[required]; !ok || required == r.SkillID {
					valid = false
				}
			}
			if r.When != nil {
				for _, t := range append(append(slices.Clone(r.When.All), r.When.Any...), r.When.None...) {
					if _, ok := c.Technologies[t]; !ok {
						valid = false
					}
				}
			}
		}
		if !valid {
			delete(c.Mappings, id)
			c.Diagnostics = append(c.Diagnostics, domain.Diagnostic{Code: "E_MAPPING_REFERENCE", Severity: "error", Message: "Excluded mapping with an invalid effective reference", Subject: id})
		}
	}
	for kind, entities := range collections {
		c.Raw[kind] = map[string]json.RawMessage{}
		for id, entity := range entities {
			active := false
			switch kind {
			case "technologies":
				_, active = c.Technologies[id]
			case "skills":
				_, active = c.Skills[id]
			case "mappings":
				_, active = c.Mappings[id]
			case "repositories":
				_, active = c.Repositories[id]
			}
			if active {
				data, err := json.Marshal(entity)
				if err != nil {
					return c, err
				}
				c.Raw[kind][id] = data
			}
		}
	}
	return c, nil
}
func mergeEntity(base, overlay domain.RawEntity, kind string, provenance map[string]string, registry string) error {
	for key, value := range overlay {
		if (key == "rules" && kind == "technologies") || (key == "recommendations" && kind == "mappings") {
			var old, incoming []domain.RawEntity
			_ = json.Unmarshal(base[key], &old)
			if err := json.Unmarshal(value, &incoming); err != nil {
				return err
			}
			if key == "recommendations" && len(incoming) == 0 {
				base[key] = value
				provenance[key] = registry
				continue
			}
			childKey := "id"
			if key == "recommendations" {
				childKey = "skillId"
			}
			for _, child := range incoming {
				var id string
				_ = json.Unmarshal(child[childKey], &id)
				for field := range child {
					provenance[key+"["+id+"]."+field] = registry
				}
				idx := slices.IndexFunc(old, func(e domain.RawEntity) bool { return string(e[childKey]) == string(child[childKey]) })
				if idx < 0 {
					old = append(old, child)
				} else {
					if key == "rules" {
						if a, b := old[idx]["kind"], child["kind"]; len(a) > 0 && len(b) > 0 && string(a) != string(b) {
							return fmt.Errorf("rule %s kind changed in registry %s (base provenance %s)", id, registry, provenance[key])
						}
						for _, pair := range [][2]string{{"glob", "pathRegex"}, {"name", "nameRegex"}, {"key", "keyRegex"}} {
							if (len(old[idx][pair[0]]) > 0 && len(child[pair[1]]) > 0) || (len(old[idx][pair[1]]) > 0 && len(child[pair[0]]) > 0) {
								return fmt.Errorf("rule %s matcher form changed in %s", id, registry)
							}
						}
					}
					for k, v := range child {
						old[idx][k] = v
					}
				}
			}
			data, _ := json.Marshal(old)
			base[key] = data
		} else {
			base[key] = value
		}
		provenance[key] = registry
	}
	return nil
}

func (s Service) run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	runner := s.Runner
	if runner == nil {
		runner = process.Runner{}
	}
	return runner.Run(ctx, dir, env, name, args...)
}

// Inspect reports cache freshness without fetching or writing application data.
func (s Service) Inspect(ref domain.RegistryRef, ttl int) (string, *string) {
	if ref.Source.Kind == "local" {
		return "local", nil
	}
	if ref.TTLHours > 0 {
		ttl = ref.TTLHours
	}
	key := storage.Hash([]byte(ref.Source.URI + "#" + ref.Source.Ref))
	directory := filepath.Join(s.Store.Cache, "registries", key)
	data, err := storage.Read(directory, "current.json", 1<<20)
	if err != nil {
		return "missing", nil
	}
	var saved struct {
		Revision string    `json:"revision"`
		Fetched  time.Time `json:"fetched"`
	}
	if json.Unmarshal(data, &saved) != nil || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(saved.Revision) {
		return "missing", nil
	}
	if _, err = readManifest(filepath.Join(directory, saved.Revision)); err != nil {
		return "missing", nil
	}
	if time.Since(saved.Fetched) < time.Duration(ttl)*time.Hour {
		return "fresh", &saved.Revision
	}
	return "stale", &saved.Revision
}
