package installation

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registry"
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

// Build turns a catalog selection into one SourcePlan per source. It performs no I/O.
func (s Service) Build(c domain.Catalog, recs catalog.Recommendations, opts Options) ([]SourcePlan, error) {
	chosen, method := s.Agents(opts)
	table, err := s.agentTable()
	if err != nil {
		return nil, err
	}
	for _, id := range chosen {
		if _, ok := table.Get(id); !ok {
			return nil, PrerequisiteError{Reason: "unsupported agent " + id}
		}
	}
	if len(chosen) == 0 {
		if opts.Yes {
			return nil, PolicyError{Reason: "non-interactive install requires --agent or configured installation.agents"}
		}
		return nil, PolicyError{Reason: "select at least one target agent"}
	}
	if method != "copy" && method != "symlink" {
		return nil, PolicyError{Reason: "invalid installation method"}
	}
	selected := unique(opts.IDs)
	ordered, err := dependencyOrder(c, recs, selected)
	if err != nil {
		return nil, err
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("select at least one skill")
	}
	if err = checkSelection(c, recs, ordered, opts.IgnoreConflicts); err != nil {
		return nil, err
	}
	plans := []SourcePlan{}
	bySource := map[string]int{}
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
			plans = append(plans, SourcePlan{Source: source, Ref: ref, Agents: slices.Clone(chosen), Method: method})
		}
		plans[at].Skills = append(plans[at].Skills, AddSkill{ID: id, Name: skill.Source.SkillName, Required: required})
	}
	return plans, nil
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
