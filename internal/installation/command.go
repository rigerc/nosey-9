package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// AddSkill is one skill installed by a SourcePlan.
type AddSkill struct {
	ID, Name string
	// Required marks a skill pulled in by another selection rather than chosen directly.
	Required bool
	// Reasons say why an update touches the skill; empty for installs.
	Reasons []string
	// RequiredBy names the installed skills that pull in a newly required skill during an
	// update; it decides which locations the skill is installed to.
	RequiredBy []string
	// Targets describes where an update writes, for the confirmation: "path (copy|symlink)".
	Targets []string
}

// SourcePlan is the install of several skills from one source and ref for one set of agents.
type SourcePlan struct {
	// Source is a git URI or, for registry-local skills, an absolute directory.
	Source string
	// Ref is the requested git ref; empty means the default branch.
	Ref    string
	Skills []AddSkill
	Agents []string
	// Method is "symlink" or "copy".
	Method string
	// Directory selects direct copies instead of agent destinations.
	Directory string
}

// Local reports whether the source is a directory inside a local registry.
func (p SourcePlan) Local() bool { return filepath.IsAbs(p.Source) }

func (p SourcePlan) names() []string {
	names := make([]string, 0, len(p.Skills))
	for _, skill := range p.Skills {
		names = append(names, skill.Name)
	}
	return names
}

// Build turns catalog selections and ad-hoc source specs into one SourcePlan per source.
// For ad-hoc sources it fetches and discovers skills to determine installable names.
func (s Service) Build(ctx context.Context, c domain.Catalog, recs catalog.Recommendations, opts Options) ([]SourcePlan, error) {
	chosen, method := s.Agents(opts)
	directory := s.Settings.Installation.Directory
	if directory != "" {
		if err := skillkit.ValidateSkillsDirectory(directory); err != nil {
			return nil, PolicyError{Reason: err.Error()}
		}
		if len(opts.Agents) > 0 {
			return nil, PolicyError{Reason: "--agent cannot be used with installation.directory; clear the setting to use agent destinations"}
		}
		directory = path.Clean(directory)
		chosen, method = nil, "copy"
	}
	table, err := s.agentTable()
	if err != nil {
		return nil, err
	}
	for _, id := range chosen {
		if _, ok := table.Get(id); !ok {
			return nil, PrerequisiteError{Reason: "unsupported agent " + id}
		}
	}
	if len(chosen) == 0 && directory == "" {
		if opts.Yes {
			return nil, PolicyError{Reason: "non-interactive install requires --agent or configured installation.agents"}
		}
		return nil, PolicyError{Reason: "select at least one target agent"}
	}
	if method != "copy" && method != "symlink" {
		return nil, PolicyError{Reason: "invalid installation method"}
	}

	plans := []SourcePlan{}
	bySource := map[string]int{}
	installNames := map[string]string{}

	if len(opts.IDs) > 0 || len(opts.Sources) == 0 {
		selected := unique(opts.IDs)
		ordered, err := dependencyOrder(c, recs, selected)
		if err != nil {
			return nil, err
		}
		if len(ordered) == 0 && len(opts.Sources) == 0 {
			return nil, fmt.Errorf("select at least one skill")
		}
		if len(ordered) > 0 {
			if err = checkSelection(c, recs, ordered, opts.IgnoreConflicts); err != nil {
				return nil, err
			}
		}
		for _, id := range ordered {
			required := !slices.Contains(selected, id)
			if required && c.InstallStatus[id] == "installed" {
				continue
			}
			skill := c.Skills[id]
			source, ref, err := addSource(c, skill)
			if err != nil {
				return nil, err
			}
			key := source + "#" + ref
			at, ok := bySource[key]
			if !ok {
				at = len(plans)
				bySource[key] = at
				plans = append(plans, SourcePlan{Source: source, Ref: ref, Agents: slices.Clone(chosen), Method: method, Directory: directory})
			}
			plans[at].Skills = append(plans[at].Skills, AddSkill{ID: id, Name: skill.Source.SkillName, Required: required})
			if other, ok := installNames[strings.ToLower(skill.Source.SkillName)]; ok && other != id {
				return nil, PolicyError{Reason: fmt.Sprintf("%s and %s both install as %q; select only one", other, id, skill.Source.SkillName)}
			}
			installNames[strings.ToLower(skill.Source.SkillName)] = id
		}
	}

	for _, spec := range opts.Sources {
		adhoc, err := s.resolveAdHocSource(ctx, spec, opts, chosen, method, directory, table, installNames)
		if err != nil {
			return nil, err
		}
		if len(adhoc.Skills) == 0 {
			continue
		}
		key := adhoc.Source + "#" + adhoc.Ref
		if at, ok := bySource[key]; ok {
			for _, skill := range adhoc.Skills {
				if !hasPlannedSkill(plans[at], skill.Name) {
					plans[at].Skills = append(plans[at].Skills, skill)
				}
			}
			continue
		}
		bySource[key] = len(plans)
		plans = append(plans, adhoc)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("select at least one skill")
	}
	return plans, nil
}

func hasPlannedSkill(plan SourcePlan, name string) bool {
	for _, s := range plan.Skills {
		if strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}

func (s Service) resolveAdHocSource(ctx context.Context, spec string, opts Options, agents []string, method, directory string, table skillkit.AgentTable, installNames map[string]string) (SourcePlan, error) {
	source, ref, local, err := parseAdHocSpec(spec, opts.Ref, s.Store.Workspace)
	if err != nil {
		return SourcePlan{}, err
	}
	src := skillkit.Source{URI: source, Ref: ref}
	if local {
		src = skillkit.Source{LocalPath: source}
	}
	checkout, err := s.fetcher().Fetch(ctx, src)
	if err != nil {
		return SourcePlan{}, fmt.Errorf("fetching %s: %w", storage.Safe(spec), err)
	}
	defer func() { _ = checkout.Close() }()
	skills, err := skillkit.Discover(checkout.Dir, skillkit.DiscoverOptions{IncludeInternal: false, FullDepth: true, Agents: table})
	var problems *skillkit.DiscoverError
	if err != nil {
		if !errors.As(err, &problems) {
			return SourcePlan{}, fmt.Errorf("discovering skills in %s: %w", storage.Safe(spec), err)
		}
		names := opts.Skills
		if len(names) == 0 {
			names = nil
		}
		if _, rerr := problems.Resolve(checkout.Dir, names); rerr != nil {
			return SourcePlan{}, fmt.Errorf("discovering skills in %s: %w", storage.Safe(spec), rerr)
		}
	}
	selected := opts.Skills
	if len(selected) == 0 {
		selected = make([]string, 0, len(skills))
		for _, sk := range skills {
			selected = append(selected, sk.Name)
		}
	}
	byName := map[string]skillkit.Skill{}
	for _, sk := range skills {
		byName[strings.ToLower(sk.Name)] = sk
	}
	plan := SourcePlan{Source: source, Ref: ref, Agents: slices.Clone(agents), Method: method, Directory: directory}
	for _, name := range selected {
		sk, ok := byName[strings.ToLower(name)]
		if !ok {
			return SourcePlan{}, fmt.Errorf("skill %q not found in %s", name, storage.Safe(spec))
		}
		if !safeSkillName.MatchString(sk.Name) {
			return SourcePlan{}, fmt.Errorf("unsafe upstream skill name %q", sk.Name)
		}
		if other, exists := installNames[strings.ToLower(sk.Name)]; exists {
			return SourcePlan{}, PolicyError{Reason: fmt.Sprintf("%s and %s both install as %q; select only one", other, spec, sk.Name)}
		}
		installNames[strings.ToLower(sk.Name)] = spec
		plan.Skills = append(plan.Skills, AddSkill{Name: sk.Name})
	}
	if len(plan.Skills) == 0 {
		return SourcePlan{}, fmt.Errorf("no skills found in %s", storage.Safe(spec))
	}
	return plan, nil
}

func parseAdHocSpec(spec, defaultRef, workspace string) (source, ref string, local bool, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false, fmt.Errorf("empty skill source")
	}
	// Local paths: absolute, relative (./, ../) or existing directories in the workspace.
	if filepath.IsAbs(spec) || strings.HasPrefix(spec, ".") || strings.ContainsRune(spec, filepath.Separator) {
		p := spec
		if !filepath.IsAbs(p) {
			p = filepath.Join(workspace, p)
		}
		if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
			abs, absErr := filepath.Abs(p)
			if absErr != nil {
				return "", "", false, absErr
			}
			return abs, "", true, nil
		}
	}
	uri, ref := spec, defaultRef
	if !strings.HasPrefix(spec, "git@") {
		if i := strings.LastIndex(spec, "@"); i > 0 {
			candidate := spec[i+1:]
			if candidate != "" && gitpolicy.ValidateRef(candidate) == nil {
				uri, ref = spec[:i], candidate
			}
		}
	}
	// A path-like argument that does not exist as a directory should be a clear error.
	if filepath.IsAbs(spec) || strings.HasPrefix(spec, ".") || strings.ContainsRune(spec, filepath.Separator) {
		if info, statErr := os.Stat(filepath.Join(workspace, spec)); statErr != nil || !info.IsDir() {
			if !filepath.IsAbs(spec) {
				if info2, err2 := os.Stat(spec); err2 != nil || !info2.IsDir() {
					return "", "", false, fmt.Errorf("local skill source %q is not a directory", spec)
				}
			} else {
				return "", "", false, fmt.Errorf("local skill source %q is not a directory", spec)
			}
		}
	}
	if err = gitpolicy.ValidateURI(uri); err != nil {
		// If it looks like a local path name, report as missing directory when not found.
		if info, statErr := os.Stat(filepath.Join(workspace, spec)); statErr == nil && info.IsDir() {
			abs, absErr := filepath.Abs(filepath.Join(workspace, spec))
			if absErr != nil {
				return "", "", false, absErr
			}
			return abs, "", true, nil
		}
		return "", "", false, fmt.Errorf("invalid skill source %q: %w", spec, err)
	}
	if err = gitpolicy.ValidateRef(ref); err != nil {
		return "", "", false, err
	}
	return uri, ref, false, nil
}

// dependencyOrder walks catalog and mapping requirements, dependencies first.
func dependencyOrder(c domain.Catalog, recs catalog.Recommendations, selected []string) ([]string, error) {
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
			if err := unavailableSkill(c, id); err != nil {
				return err
			}
			return fmt.Errorf("unknown required skill %s", id)
		}
		visit[id] = 1
		for _, dep := range unique(append(slices.Clone(skill.Requires), recs.Requires[id]...)) {
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
			return nil, err
		}
	}
	return ordered, nil
}

// unavailableSkill explains why a declared skill cannot be installed, or returns
// nil when the catalog does not know it at all.
func unavailableSkill(c domain.Catalog, id string) error {
	if skill, ok := c.Quarantined[id]; ok {
		reason := "upstream is gone"
		if skill.Quarantine != nil {
			reason = skill.Quarantine.Reason
		}
		return PolicyError{Reason: "skill is quarantined: " + reason + " (" + id + ")"}
	}
	if reason, ok := c.Blocked[id]; ok {
		return PolicyError{Reason: "skill is unavailable: " + reason + " (" + id + ")"}
	}
	return nil
}

// checkSelection blocks what the upstream installer cannot detect across separate source runs.
func checkSelection(c domain.Catalog, recs catalog.Recommendations, ordered []string, ignoreConflicts bool) error {
	names := map[string]string{}
	problems := []string{}
	for _, id := range ordered {
		key := strings.ToLower(c.Skills[id].Source.SkillName)
		if other, ok := names[key]; ok {
			return PolicyError{Reason: fmt.Sprintf("%s and %s both install as %q; select only one", other, id, c.Skills[id].Source.SkillName)}
		}
		names[key] = id
		for _, conflict := range unique(append(slices.Clone(c.Skills[id].ConflictsWith), recs.Conflicts[id]...)) {
			switch status := c.InstallStatus[conflict]; {
			case slices.Contains(ordered, conflict):
				problems = append(problems, id+" conflicts with selected "+conflict)
			case status == "installed" || status == "outdated":
				problems = append(problems, id+" conflicts with installed "+conflict)
			}
		}
	}
	if len(problems) > 0 && !ignoreConflicts {
		return PolicyError{Reason: strings.Join(unique(problems), "; ")}
	}
	return nil
}

func addSource(c domain.Catalog, skill domain.Skill) (source, ref string, err error) {
	if !safeSkillName.MatchString(skill.Source.SkillName) {
		return "", "", fmt.Errorf("unsafe upstream skill name %q", skill.Source.SkillName)
	}
	if skill.Source.Kind == "local" {
		root := c.Roots[skill.RegistryID]
		if !filepath.IsAbs(root) || !filepath.IsLocal(skill.Source.Path) {
			return "", "", fmt.Errorf("local skill path must remain inside registry")
		}
		return filepath.Join(root, skill.Source.Path), "", nil
	}
	if err = registry.ValidateGit(skill.Source.URI, skill.Source.Ref); err != nil {
		return "", "", err
	}
	return skill.Source.URI, skill.Source.Ref, nil
}

// Summary describes what a confirmed source will install, for the confirmation prompt.
// Values are sanitized for terminal display.
func (p SourcePlan) Summary() string {
	names := make([]string, 0, len(p.Skills))
	for _, skill := range p.Skills {
		name := storage.Safe(skill.Name)
		if skill.Required {
			name += " (required)"
		}
		names = append(names, name)
	}
	ref := p.Ref
	if ref == "" {
		ref = "default branch"
	}
	if p.Directory != "" {
		return fmt.Sprintf("Source: %s\n  Ref: %s\n  Skills: %s\n  Directory: %s/<skill> (direct copy)", storage.Safe(p.Source), storage.Safe(ref), strings.Join(names, ", "), storage.Safe(p.Directory))
	}
	return fmt.Sprintf("Source: %s\n  Ref: %s\n  Skills: %s\n  Agents: %s\n  Method: %s",
		storage.Safe(p.Source), storage.Safe(ref), strings.Join(names, ", "), storage.Safe(strings.Join(p.Agents, ", ")), p.Method)
}

// Agents returns the agent IDs and method a Build with these options and settings selects.
func (s Service) Agents(opts Options) (agents []string, method string) {
	agents = unique(opts.Agents)
	if len(agents) == 0 {
		agents = unique(s.Settings.Installation.Agents)
	}
	method = opts.Method
	if method == "" {
		method = s.Settings.Installation.Method
	}
	return agents, method
}

// Label is a short, sanitized description of the source and ref for progress output.
func (p SourcePlan) Label() string {
	label := strings.TrimSuffix(strings.TrimPrefix(p.Source, "https://github.com/"), ".git")
	if ref := p.Ref; ref != "" {
		if len(ref) == 40 {
			ref = ref[:7]
		}
		label += " @" + ref
	}
	return storage.Safe(label)
}
