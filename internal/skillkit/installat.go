package skillkit

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// InstallAt places a skill at exactly the given locations instead of deriving them from agents.
// It refreshes what is already on disk (see InstalledSkill.Locations) and preserves each
// location's mode: a copy location is replaced by a copy, a symlink location stays a symlink to
// the canonical copy. The canonical copy in .agents/skills is written first whenever a location
// is canonical or a symlink. Every write goes through os.Root. Outcomes are reported per
// location in the order given, with Agent holding the comma-joined attributed agents (empty when
// none is detected), and the result is complete even when an error is returned.
func InstallAt(ctx context.Context, skill Skill, locs []Location, target Target) (InstallResult, error) {
	res := InstallResult{Skill: skill.Name, Status: StatusNotStarted}
	canonical := ""
	if err := target.validate(); err != nil {
		res.Err = err
		return res, err
	}
	name := SanitizeName(skill.Name)
	if !filepath.IsLocal(name) {
		res.Err = errors.New("invalid skill name: potential path traversal")
		return res, res.Err
	}
	canonical = canonicalRel(name)
	known := append(target.Agents.SkillsDirs(), UniversalSkillsDir)
	needCanonical := false
	for _, l := range locs {
		mode := ModeCopy
		if l.Symlink && l.Path != canonical {
			mode = ModeSymlink
		}
		out := AgentOutcome{Agent: strings.Join(l.Agents, ","), Path: l.Path, Mode: mode, Status: StatusNotStarted}
		if mode == ModeSymlink {
			out.CanonicalPath = canonical
		}
		res.Agents = append(res.Agents, out)
		if l.Path == canonical || mode == ModeSymlink {
			needCanonical = true
		}
		if !slices.Contains(known, l.Dir) || path.Dir(l.Path) != l.Dir || !filepath.IsLocal(filepath.FromSlash(l.Path)) {
			err := &PolicyError{Path: l.Path, Reason: "not a skills directory of a known agent"}
			res.Status, res.Err = StatusFailed, err
			return res, err
		}
	}
	if len(locs) == 0 {
		res.Err = errors.New("no install locations")
		return res, res.Err
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
	if err := ctx.Err(); err != nil {
		res.Err = err
		return res, err
	}
	var err error
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
	st := &installState{ctx: ctx, dest: dest, src: skill.Dir, realSrc: realSrc, name: name, target: target, res: &res, done: map[string]*AgentOutcome{}}

	var canonicalErr *AgentOutcome
	if needCanonical {
		switch {
		case realSrc == dest.realPath(canonical):
			// Refreshing from the canonical directory itself: nothing to copy.
		case st.overlaps(canonical):
			canonicalErr = &AgentOutcome{Status: StatusSkipped, Reason: ReasonSourceOverlaps}
		default:
			if cerr := st.copyTo(canonical); cerr != nil {
				canonicalErr = &AgentOutcome{Status: StatusFailed, Err: cerr}
			}
		}
	}
	var errs []error
	for i, l := range locs {
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		out := res.Agents[i]
		switch {
		case l.Path == canonical || out.Mode == ModeCopy:
			switch {
			case l.Path == canonical && canonicalErr != nil:
				out.Status, out.Reason, out.Err = canonicalErr.Status, canonicalErr.Reason, canonicalErr.Err
			case l.Path == canonical:
				out.Status = StatusInstalled
			case st.overlaps(l.Path):
				out.Status, out.Reason = StatusSkipped, ReasonSourceOverlaps
			default:
				if cerr := st.copyTo(l.Path); cerr != nil {
					out.Status, out.Err = StatusFailed, cerr
				} else {
					out.Status = StatusInstalled
				}
			}
		case canonicalErr != nil:
			out.Status, out.Reason, out.Err = canonicalErr.Status, canonicalErr.Reason, canonicalErr.Err
		default:
			fallback, lerr := st.link(canonical, l.Path)
			switch {
			case lerr == nil:
				out.Status = StatusInstalled
			case fallback:
				if cerr := st.copyTo(l.Path); cerr != nil {
					out.Status, out.Err = StatusFailed, cerr
				} else {
					out.Status, out.SymlinkFallback = StatusInstalled, true
				}
			default:
				out.Status, out.Err = StatusFailed, lerr
			}
		}
		res.Agents[i] = out
		if out.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", out.Path, out.Err))
		}
	}
	res.Warnings = st.warnings
	res.Status = summarize(res.Agents)
	res.Err = errors.Join(errs...)
	return res, res.Err
}
