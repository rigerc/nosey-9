package skillkit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Mode is how a skill reaches an agent directory.
type Mode string

// Install modes.
const (
	// ModeSymlink copies the skill once into .agents/skills and links agent directories to it.
	ModeSymlink Mode = "symlink"
	// ModeCopy copies the skill into every agent directory.
	ModeCopy Mode = "copy"
)

// Status is the outcome of installing one skill for one agent, or of a whole skill.
type Status string

// Outcome statuses.
const (
	StatusInstalled  Status = "installed"
	StatusFailed     Status = "failed"
	StatusSkipped    Status = "skipped"
	StatusNotStarted Status = "not-started"
)

// Skip reasons reported in AgentOutcome.Reason.
const (
	ReasonMissingAgentDir = "missing-agent-project-directory"
	ReasonSourceOverlaps  = "source-overlaps-destination"
)

// Target says where installed skills live.
type Target struct {
	// Workspace is the project directory; it must exist.
	Workspace string
	// Mode defaults to ModeSymlink.
	Mode Mode
	// Agents is the agent table shared by discovery, install, list and uninstall.
	Agents AgentTable
	// CreateMissingAgentRoot installs symlink-mode links for built-in agents whose
	// project directory (for example .windsurf) does not exist yet. By default such
	// agents are skipped, as in skills@1.7.6.
	CreateMissingAgentRoot bool
}

func (t Target) mode() Mode {
	if t.Mode == "" {
		return ModeSymlink
	}
	return t.Mode
}

func (t Target) validate() error {
	if t.Workspace == "" {
		return errors.New("workspace is required")
	}
	if m := t.mode(); m != ModeSymlink && m != ModeCopy {
		return fmt.Errorf("unknown install mode %q", m)
	}
	if t.Agents.agents == nil {
		return errors.New("agent table is required")
	}
	return nil
}

// AgentOutcome is what happened for one agent.
type AgentOutcome struct {
	Agent  string
	Status Status
	// Path is the workspace-relative directory the agent reads the skill from.
	Path string
	// CanonicalPath is the shared copy in symlink mode.
	CanonicalPath string
	// Mode is how the skill was actually installed for this agent.
	Mode Mode
	// SymlinkFallback is set when linking failed and the skill was copied instead.
	SymlinkFallback bool
	// Reason explains a skip.
	Reason string
	Err    error
}

// InstallResult is the outcome of installing one skill.
type InstallResult struct {
	// Skill is the skill's name.
	Skill string
	// Status is installed if any agent installed, failed if none did and one failed,
	// skipped if every agent was skipped, else not-started.
	Status Status
	Agents []AgentOutcome
	// Hash is FolderHash of the source skill, what skills@1.7.6 records as computedHash.
	Hash     string
	Warnings []Warning
	Err      error
}

// Installed reports whether at least one agent received the skill.
func (r InstallResult) Installed() bool { return r.Status == StatusInstalled }

// UnknownAgentError reports agent IDs absent from the table.
type UnknownAgentError struct{ IDs []string }

func (e *UnknownAgentError) Error() string { return "unknown agent: " + strings.Join(e.IDs, ", ") }

// MissingSkillError reports requested skills the source does not contain.
type MissingSkillError struct{ Names []string }

func (e *MissingSkillError) Error() string {
	return "skill not found in source: " + strings.Join(e.Names, ", ")
}

func canonicalRel(name string) string { return path.Join(UniversalSkillsDir, name) }

// Install installs one skill for the given agents. It always returns the
// per-agent outcomes, together with an error if any agent failed or ctx ended;
// agents not reached after cancellation are reported as not-started.
func Install(ctx context.Context, skill Skill, agentIDs []string, target Target) (InstallResult, error) {
	res := InstallResult{Skill: skill.Name, Status: StatusNotStarted}
	ids, err := resolveAgents(agentIDs, target)
	for _, id := range ids {
		res.Agents = append(res.Agents, AgentOutcome{Agent: id, Status: StatusNotStarted})
	}
	if err != nil {
		res.Err = err
		return res, err
	}
	fail := func(err error) (InstallResult, error) {
		res.Status, res.Err = StatusFailed, err
		for i := range res.Agents {
			if res.Agents[i].Status == StatusNotStarted {
				res.Agents[i].Status, res.Agents[i].Err = StatusFailed, err
			}
		}
		return res, err
	}
	if err = ctx.Err(); err != nil {
		res.Err = err
		return res, err
	}
	if res.Hash, err = FolderHash(skill.Dir); err != nil {
		return fail(fmt.Errorf("hashing %s: %w", skill.Name, err))
	}
	realSrc, err := filepath.EvalSymlinks(skill.Dir)
	if err != nil {
		return fail(err)
	}
	dest, err := openDest(target.Workspace)
	if err != nil {
		return fail(err)
	}
	defer dest.Close()

	name := SanitizeName(skill.Name)
	if !filepath.IsLocal(name) {
		return fail(errors.New("invalid skill name: potential path traversal"))
	}
	st := &installState{ctx: ctx, dest: dest, src: skill.Dir, realSrc: realSrc, name: name, target: target, res: &res, done: map[string]*AgentOutcome{}}
	var errs []error
	for i, id := range ids {
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		agent, _ := target.Agents.Get(id)
		out := st.installAgent(agent)
		res.Agents[i] = out
		if out.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, out.Err))
		}
	}
	res.Warnings = st.warnings
	res.Status = summarize(res.Agents)
	res.Err = errors.Join(errs...)
	return res, res.Err
}

func summarize(outs []AgentOutcome) Status {
	var installed, failed, skipped int
	for _, o := range outs {
		switch o.Status {
		case StatusInstalled:
			installed++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	switch {
	case installed > 0:
		return StatusInstalled
	case failed > 0:
		return StatusFailed
	case skipped > 0:
		return StatusSkipped
	}
	return StatusNotStarted
}

// resolveAgents drops duplicate IDs and rejects unknown ones, keeping caller order.
func resolveAgents(ids []string, target Target) ([]string, error) {
	if err := target.validate(); err != nil {
		return nil, err
	}
	var out, unknown []string
	for _, id := range ids {
		if _, ok := target.Agents.Get(id); !ok {
			unknown = append(unknown, id)
		} else if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if len(unknown) > 0 {
		return out, &UnknownAgentError{IDs: unknown}
	}
	return out, nil
}

type installState struct {
	ctx      context.Context
	dest     *destRoot
	src      string
	realSrc  string
	name     string
	target   Target
	res      *InstallResult
	warnings []Warning

	canonicalReady bool
	canonicalOut   *AgentOutcome // non-nil when canonical could not be provided
	done           map[string]*AgentOutcome
}

func (s *installState) copyTo(rel string) error {
	var warns []Warning
	err := s.dest.replace(rel, func(tmp string) error {
		w, cerr := s.dest.copySkill(s.ctx, s.src, tmp)
		warns = w
		return cerr
	})
	if err == nil {
		s.warnings = append(s.warnings, warns...)
	}
	return err
}

// overlaps ports pathsOverlap: the source tree and destination contain one another.
func (s *installState) overlaps(destRel string) bool {
	d := s.dest.realPath(destRel)
	return within(s.realSrc, d) || within(d, s.realSrc)
}

func (s *installState) installAgent(a Agent) AgentOutcome {
	canonical := canonicalRel(s.name)
	agentRel := path.Join(a.SkillsDir, s.name)
	if a.Universal() {
		agentRel = canonical
	}
	out := AgentOutcome{Agent: a.ID, Path: agentRel, Mode: s.target.mode()}
	if s.target.mode() == ModeSymlink {
		out.CanonicalPath = canonical
	}
	if prev, ok := s.done[agentRel]; ok && s.target.mode() == ModeCopy {
		shared := *prev
		shared.Agent = a.ID
		return shared
	}
	if (s.target.mode() == ModeCopy || agentRel != canonical) && s.overlaps(agentRel) {
		out.Status, out.Reason = StatusSkipped, ReasonSourceOverlaps
		return out
	}
	if s.target.mode() == ModeCopy {
		if err := s.copyTo(agentRel); err != nil {
			out.Status, out.Err = StatusFailed, err
			return out
		}
		out.Status = StatusInstalled
		s.done[agentRel] = &out
		return out
	}

	// Symlink mode: provide the canonical copy once.
	if !s.canonicalReady {
		s.canonicalReady = true
		switch {
		case s.realSrc == s.dest.realPath(canonical):
			// Installing from the canonical directory itself: nothing to copy.
		case s.overlaps(canonical):
			c := AgentOutcome{Status: StatusSkipped, Reason: ReasonSourceOverlaps}
			s.canonicalOut = &c
		default:
			if err := s.copyTo(canonical); err != nil {
				c := AgentOutcome{Status: StatusFailed, Err: err}
				s.canonicalOut = &c
			}
		}
	}
	if c := s.canonicalOut; c != nil {
		out.Status, out.Reason, out.Err, out.Path = c.Status, c.Reason, c.Err, canonical
		return out
	}
	if a.Universal() {
		out.Status = StatusInstalled
		return out
	}
	if !a.Custom && !a.createProjectDir && !s.target.CreateMissingAgentRoot {
		root, _, _ := strings.Cut(a.SkillsDir, "/")
		if _, err := os.Stat(filepath.Join(s.dest.real, root)); err != nil {
			out.Status, out.Reason, out.Path = StatusSkipped, ReasonMissingAgentDir, canonical
			return out
		}
	}
	fallback, err := s.link(canonical, agentRel)
	switch {
	case err == nil:
		out.Status = StatusInstalled
	case fallback:
		if cerr := s.copyTo(agentRel); cerr != nil {
			out.Status, out.Err = StatusFailed, cerr
			return out
		}
		out.Status, out.SymlinkFallback = StatusInstalled, true
	default:
		out.Status, out.Err = StatusFailed, err
	}
	return out
}

// link ports createSymlink: a relative link at agentRel to canonical, a no-op
// if it already resolves there. A policy error is final; any other failure
// asks the caller to fall back to copying.
func (s *installState) link(canonical, agentRel string) (fallback bool, err error) {
	realCanonical := s.dest.realPath(canonical)
	if s.dest.realPath(agentRel) == realCanonical {
		return false, nil
	}
	if err = s.dest.checkParent(agentRel); err != nil {
		return false, err
	}
	rel, err := filepath.Rel(s.dest.realPath(path.Dir(agentRel)), realCanonical)
	if err != nil {
		return true, err
	}
	err = s.dest.replace(agentRel, func(tmp string) error {
		return s.dest.root.Symlink(rel, filepath.FromSlash(tmp))
	})
	var pe *PolicyError
	return err != nil && !errors.As(err, &pe), err
}
