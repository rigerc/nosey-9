package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/storage"
)

// AddSkill is one skill installed by an AddCommand.
type AddSkill struct {
	ID, Name string
	// Required marks a skill pulled in by another selection rather than chosen directly.
	Required bool
}

// AddCommand is one upstream `skills add` invocation for a single source.
type AddCommand struct {
	Source string
	Ref    string
	Skills []AddSkill
	Args   []string
}

// Build turns a catalog selection into one `skills add` per source. It performs no I/O.
func (s Service) Build(c domain.Catalog, recs catalog.Recommendations, opts Options) ([]AddCommand, error) {
	if s.Settings.Installation.Version != Version {
		return nil, PrerequisiteError{Reason: "only skills@" + Version + " is supported"}
	}
	chosen := unique(opts.Agents)
	if len(chosen) == 0 {
		chosen = unique(s.Settings.Installation.Agents)
	}
	for _, id := range chosen {
		if _, ok := agents[id]; !ok {
			return nil, PrerequisiteError{Reason: "unsupported agent " + id}
		}
	}
	if opts.Yes && len(chosen) == 0 {
		return nil, fmt.Errorf("non-interactive install requires --agent or configured installation.agents")
	}
	method := opts.Method
	if method == "" {
		method = s.Settings.Installation.Method
	}
	if method != "copy" && method != "symlink" {
		return nil, fmt.Errorf("invalid installation method")
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
	commands := []AddCommand{}
	bySource := map[string]int{}
	for _, id := range ordered {
		required := !slices.Contains(selected, id)
		if required && c.InstallStatus[id] == "installed" {
			continue
		}
		skill := c.Skills[id]
		source, ref, arg, err := addSource(c, skill)
		if err != nil {
			return nil, err
		}
		at, ok := bySource[arg]
		if !ok {
			at = len(commands)
			bySource[arg] = at
			commands = append(commands, AddCommand{Source: source, Ref: ref})
		}
		commands[at].Skills = append(commands[at].Skills, AddSkill{ID: id, Name: skill.Source.SkillName, Required: required})
		commands[at].Args = addArgs(arg, commands[at].Skills, chosen, method, opts.Yes)
	}
	return commands, nil
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

func addSource(c domain.Catalog, skill domain.Skill) (source, ref, arg string, err error) {
	if !safeSkillName.MatchString(skill.Source.SkillName) {
		return "", "", "", fmt.Errorf("unsafe upstream skill name %q", skill.Source.SkillName)
	}
	if skill.Source.Kind == "local" {
		root := c.Roots[skill.RegistryID]
		if !filepath.IsAbs(root) || !filepath.IsLocal(skill.Source.Path) {
			return "", "", "", fmt.Errorf("local skill path must remain inside registry")
		}
		path := filepath.Join(root, skill.Source.Path)
		return path, "", path, nil
	}
	if err = registry.ValidateGit(skill.Source.URI, skill.Source.Ref); err != nil {
		return "", "", "", err
	}
	arg = skill.Source.URI
	if skill.Source.Ref != "" {
		arg += "#" + skill.Source.Ref
	}
	return skill.Source.URI, skill.Source.Ref, arg, nil
}

func addArgs(source string, skills []AddSkill, chosen []string, method string, yes bool) []string {
	args := []string{"add", source, "--full-depth", "--skill"}
	for _, skill := range skills {
		args = append(args, skill.Name)
	}
	if len(chosen) > 0 {
		args = append(append(args, "--agent"), chosen...)
	}
	if method == "copy" {
		args = append(args, "--copy")
	}
	if yes {
		args = append(args, "--yes")
	}
	return args
}

// Text returns the shell-escaped command for a user to run themselves.
func (c AddCommand) Text() string {
	parts := []string{}
	for _, arg := range append([]string{"npx", "--yes", "skills@" + Version}, c.Args...) {
		parts = append(parts, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
	}
	return strings.Join(parts, " ")
}

// Prepare checks the project and resolves the CLI before any source runs.
func (s Service) Prepare(ctx context.Context) (CLI, error) {
	if err := s.Preflight(); err != nil {
		return CLI{}, err
	}
	if err := s.requirePrerequisites(ctx); err != nil {
		return CLI{}, err
	}
	return s.resolveCLI(ctx)
}

// RunSource runs a confirmed source without terminal access and verifies its lock entries.
// Output is bounded by the runner; only a short, sanitized failure detail is returned.
func (s Service) RunSource(ctx context.Context, cli CLI, command AddCommand) error {
	if !slices.Contains(command.Args, "--yes") {
		return PolicyError{Reason: "background installation requires --yes"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Runner == nil {
		s.Runner = process.Runner{}
	}
	if runner, ok := s.Runner.(process.Runner); ok {
		if runner.Timeout == 0 {
			runner.Timeout = 5 * time.Minute
		}
		if runner.Limit == 0 {
			runner.Limit = 256 << 10
		}
		s.Runner = runner
	}
	name, args := cli.argv(command.Args...)
	out, err := s.run(ctx, s.Store.Workspace, s.env(), name, args...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		detail := strings.TrimSpace(string(out.Stderr))
		if detail == "" {
			detail = strings.TrimSpace(string(out.Stdout))
		}
		detail = storage.Safe(ansi.Strip(detail))
		if len(detail) > 2000 {
			detail = string([]rune(detail)[max(0, len([]rune(detail))-1000):])
		}
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return s.VerifyInstall(command)
}

// Install runs each command on the terminal and stops at the first failure.
func (s Service) Install(ctx context.Context, commands []AddCommand) error {
	cli, err := s.Prepare(ctx)
	if err != nil {
		return err
	}
	attach := s.Attach
	if attach == nil {
		attach = process.RunAttached
	}
	for i, command := range commands {
		name, args := cli.argv(command.Args...)
		if err = attach(ctx, s.Store.Workspace, s.env(), name, args...); err != nil {
			return fmt.Errorf("skills add failed for %s: %w (%d later sources not started)", command.Source, err, len(commands)-i-1)
		}
		if err = s.VerifyInstall(command); err != nil {
			return fmt.Errorf("%w (%d later sources not started)", err, len(commands)-i-1)
		}
	}
	return nil
}

// VerifyInstall catches what a zero exit hides: without prompts, upstream skips skills it cannot find.
// Interactive runs are excluded because a cancelled prompt also exits zero and is not a failure.
func (s Service) VerifyInstall(command AddCommand) error {
	if !slices.Contains(command.Args, "--yes") {
		return nil
	}
	lock, err := s.readLock()
	if err != nil {
		return fmt.Errorf("skills add for %s left no usable lock: %w", command.Source, err)
	}
	missing := []string{}
	for _, skill := range command.Skills {
		if _, ok := lock.Skills[skill.Name]; !ok {
			missing = append(missing, skill.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("skills CLI did not install %s from %s", strings.Join(missing, ", "), command.Source)
	}
	return nil
}

// Preflight refuses project roots and lock states the installer must not touch.
func (s Service) Preflight() error {
	if home, err := os.UserHomeDir(); err == nil && samePath(home, s.Store.Workspace) {
		return PolicyError{Reason: "refusing to install into the home directory; run from a project directory"}
	}
	if _, err := s.readLock(); err != nil && !errors.Is(err, errLockMissing) {
		return PolicyError{Reason: err.Error() + "; fix or delete it before installing"}
	}
	return nil
}

func samePath(a, b string) bool {
	left, leftErr := filepath.EvalSymlinks(a)
	right, rightErr := filepath.EvalSymlinks(b)
	return leftErr == nil && rightErr == nil && left == right
}
