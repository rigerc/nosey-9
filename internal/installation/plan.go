package installation

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/storage"
)

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
			sha, err := s.gitSource(ctx, logical, skill.Source.Ref)
			if err != nil {
				return p, err
			}
			revision = &sha
			// The resolved commit SHA is the content identity; the upstream skills CLI fetches that exact revision.
			hash = sha
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
		if skill.Source.Kind == "git" && (ref == nil || !fullSHARef.MatchString(*ref)) {
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
