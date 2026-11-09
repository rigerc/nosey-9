package skillkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func skillIn(t *testing.T, name string) Skill {
	t.Helper()
	dir := writeSkill(t, filepath.Join(t.TempDir(), "src", name), name, map[string]string{"scripts/run.sh": "echo hi\n"})
	return mustDiscover(t, dir, DiscoverOptions{})[0]
}

func target(t *testing.T, mode Mode, custom ...CustomAgent) (Target, string) {
	t.Helper()
	table, _ := testTable(t, custom...)
	ws := t.TempDir()
	return Target{Workspace: ws, Mode: mode, Agents: table}, ws
}

func TestInstallSymlinkModeSharesCanonicalDirectory(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".claude"), 0o755))
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"codex", "claude-code", "cursor"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Status)
	require.Len(t, res.Agents, 3)
	for _, a := range res.Agents {
		require.Equal(t, StatusInstalled, a.Status, a.Agent)
	}
	require.FileExists(t, filepath.Join(ws, ".agents/skills/demo/scripts/run.sh"))
	link, err := os.Readlink(filepath.Join(ws, ".claude/skills/demo"))
	require.NoError(t, err)
	require.Equal(t, filepath.FromSlash("../../.agents/skills/demo"), link, "links are relative")
	require.FileExists(t, filepath.Join(ws, ".claude/skills/demo/SKILL.md"))
	require.Equal(t, ".agents/skills/demo", res.Agents[0].Path)
	require.Equal(t, ".claude/skills/demo", res.Agents[1].Path)
	require.NotEmpty(t, res.Hash)
	hash, err := FolderHash(filepath.Join(ws, ".agents/skills/demo"))
	require.NoError(t, err)
	require.Equal(t, res.Hash, hash, "installed tree hashes like its source")
}

func TestInstallSkipsAgentsWhoseProjectDirectoryIsMissing(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"windsurf", "claude-code", "codex"}, tg)
	require.NoError(t, err)
	byAgent := map[string]AgentOutcome{}
	for _, a := range res.Agents {
		byAgent[a.Agent] = a
	}
	require.Equal(t, StatusSkipped, byAgent["windsurf"].Status)
	require.Equal(t, ReasonMissingAgentDir, byAgent["windsurf"].Reason)
	require.NoDirExists(t, filepath.Join(ws, ".windsurf"))
	require.Equal(t, StatusInstalled, byAgent["claude-code"].Status, "claude-code always creates its directory")
	require.Equal(t, StatusInstalled, byAgent["codex"].Status)

	tg.CreateMissingAgentRoot = true
	res, err = Install(context.Background(), skillIn(t, "demo"), []string{"windsurf"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Agents[0].Status)
	require.FileExists(t, filepath.Join(ws, ".windsurf/skills/demo/SKILL.md"))
}

func TestInstallAllSkippedIsNotInstalled(t *testing.T) {
	tg, _ := target(t, ModeSymlink)
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"windsurf"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusSkipped, res.Status)
	require.False(t, res.Installed())
}

func TestInstallCopyModeCopiesPerDirectory(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"claude-code", "codex", "cursor"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Status)
	for _, p := range []string{".claude/skills/demo", ".agents/skills/demo"} {
		info, err := os.Lstat(filepath.Join(ws, p))
		require.NoError(t, err)
		require.True(t, info.IsDir() && info.Mode()&os.ModeSymlink == 0, p+" must be a real directory")
	}
	require.Equal(t, res.Agents[1].Path, res.Agents[2].Path, "universal agents share one directory")
	require.Empty(t, res.Agents[1].CanonicalPath)
}

func TestInstallReplacesExistingInstallAtomically(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	skill := skillIn(t, "demo")
	_, err := Install(context.Background(), skill, []string{"claude-code"}, tg)
	require.NoError(t, err)
	stale := filepath.Join(ws, ".claude/skills/demo/stale.txt")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))
	_, err = Install(context.Background(), skill, []string{"claude-code"}, tg)
	require.NoError(t, err)
	require.NoFileExists(t, stale, "update replaces the whole directory")
	entries, err := os.ReadDir(filepath.Join(ws, ".claude/skills"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary or backup directories remain")

	// A failed copy leaves the previous install untouched.
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(filepath.Join(skill.Dir, "scripts/run.sh"), 0))
	if os.Geteuid() != 0 {
		_, err = Install(context.Background(), skill, []string{"claude-code"}, tg)
		require.Error(t, err)
		require.FileExists(t, stale)
		entries, err = os.ReadDir(filepath.Join(ws, ".claude/skills"))
		require.NoError(t, err)
		require.Len(t, entries, 1, "failed install cleans up its temporary directory")
	}
}

func TestInstallReplacesExistingSymlinkAndDirectory(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".claude/skills/demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".claude/skills/demo/old.txt"), []byte("x"), 0o644))
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"claude-code"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Status)
	_, err = os.Readlink(filepath.Join(ws, ".claude/skills/demo"))
	require.NoError(t, err, "a real directory is replaced by the link")
	// Re-running is a no-op that keeps the link.
	res, err = Install(context.Background(), skillIn(t, "demo"), []string{"claude-code"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Agents[0].Status)
}

func TestInstallSkillsRequestedSkillMissingIsAnError(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	have := []Skill{skillIn(t, "present")}
	report, err := InstallSkills(context.Background(), []string{"present", "absent"}, have, []string{"codex"}, tg)
	var missing *MissingSkillError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, []string{"absent"}, missing.Names)
	require.NoDirExists(t, filepath.Join(ws, ".agents"), "nothing is installed when a request cannot be satisfied")
	require.Len(t, report.Skills, 2)
	require.Equal(t, StatusNotStarted, report.Skills[0].Status)
	require.Equal(t, StatusFailed, report.Skills[1].Status)
	require.Empty(t, report.Installed())
}

func TestInstallSkillsSelectsByNameCaseInsensitively(t *testing.T) {
	tg, _ := target(t, ModeSymlink)
	have := []Skill{skillIn(t, "alpha"), skillIn(t, "beta")}
	report, err := InstallSkills(context.Background(), []string{"BETA"}, have, []string{"codex"}, tg)
	require.NoError(t, err)
	require.Len(t, report.Skills, 1)
	require.Equal(t, "beta", report.Skills[0].Skill)
	all, err := InstallSkills(context.Background(), nil, have, []string{"codex"}, tg)
	require.NoError(t, err)
	require.Len(t, all.Installed(), 2)
}

func TestInstallUnknownAgentFailsBeforeWriting(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"codex", "nope"}, tg)
	var unknown *UnknownAgentError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, []string{"nope"}, unknown.IDs)
	require.Equal(t, StatusNotStarted, res.Agents[0].Status)
	require.NoDirExists(t, filepath.Join(ws, ".agents"))
}

func TestInstallHonoursCancellationBetweenSkillsAndAgents(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	have := []Skill{skillIn(t, "one"), skillIn(t, "two"), skillIn(t, "three")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := InstallSkills(ctx, nil, have, []string{"claude-code", "codex"}, tg)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, report.Skills, 3)
	for _, s := range report.Skills {
		require.Equal(t, StatusNotStarted, s.Status)
		for _, a := range s.Agents {
			require.Equal(t, StatusNotStarted, a.Status)
		}
	}
	require.NoDirExists(t, filepath.Join(ws, ".claude"))

	// Cancelling mid-run keeps completed work and reports the rest as not started.
	ctx, cancel = context.WithCancel(context.Background())
	hook := &cancelAfter{cancel: cancel}
	report = Report{}
	var firstErr error
	for i, s := range have {
		if i == 1 {
			hook.fire()
		}
		res, err := Install(ctx, s, []string{"claude-code", "codex"}, tg)
		report.Skills = append(report.Skills, res)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	require.ErrorIs(t, firstErr, context.Canceled)
	require.Equal(t, StatusInstalled, report.Skills[0].Status)
	require.Equal(t, StatusNotStarted, report.Skills[1].Status)
	require.DirExists(t, filepath.Join(ws, ".claude/skills/one"))
	require.NoDirExists(t, filepath.Join(ws, ".claude/skills/two"))
}

type cancelAfter struct{ cancel context.CancelFunc }

func (c *cancelAfter) fire() { c.cancel() }

func TestInstallReturnsPerAgentFailuresWithTheError(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	// A regular file where a directory is needed makes one agent fail while another succeeds.
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".claude"), []byte("not a dir"), 0o644))
	res, err := Install(context.Background(), skillIn(t, "demo"), []string{"claude-code", "codex"}, tg)
	require.Error(t, err)
	require.Equal(t, StatusInstalled, res.Status, "a skill that reached one agent is recorded")
	byAgent := map[string]AgentOutcome{}
	for _, a := range res.Agents {
		byAgent[a.Agent] = a
	}
	require.Equal(t, StatusFailed, byAgent["claude-code"].Status)
	require.Error(t, byAgent["claude-code"].Err)
	require.Equal(t, StatusInstalled, byAgent["codex"].Status)
	require.Error(t, res.Err)
}

func TestInstallSourceInsideDestinationIsSkipped(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	dir := writeSkill(t, filepath.Join(ws, ".agents/skills/self"), "self", nil)
	skill := mustDiscover(t, dir, DiscoverOptions{})[0]
	res, err := Install(context.Background(), skill, []string{"codex"}, tg)
	require.NoError(t, err)
	require.Equal(t, StatusSkipped, res.Agents[0].Status)
	require.Equal(t, ReasonSourceOverlaps, res.Agents[0].Reason)
	require.FileExists(t, filepath.Join(dir, "SKILL.md"), "the source is untouched")
}

// TestInstallRefusesSymlinkedAncestorsThatLeaveTheWorkspace covers built-in and custom agents.
func TestInstallRefusesSymlinkedAncestorsThatLeaveTheWorkspace(t *testing.T) {
	for _, mode := range []Mode{ModeSymlink, ModeCopy} {
		for name, setup := range map[string]struct {
			agent  string
			custom []CustomAgent
			link   string // workspace-relative symlink created to the outside directory
		}{
			"built-in agent dir":        {agent: "claude-code", link: ".claude"},
			"built-in nested skills":    {agent: "claude-code", link: ".claude/skills"},
			"canonical dir":             {agent: "codex", link: ".agents"},
			"custom agent dir":          {agent: "my-agent", custom: []CustomAgent{{ID: "my-agent", Name: "M", SkillsDir: ".my/skills"}}, link: ".my"},
			"custom agent nested ances": {agent: "my-agent", custom: []CustomAgent{{ID: "my-agent", Name: "M", SkillsDir: "tools/x/skills"}}, link: "tools"},
		} {
			t.Run(string(mode)+"/"+name, func(t *testing.T) {
				tg, ws := target(t, mode, setup.custom...)
				outside := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(ws, filepath.Dir(setup.link)), 0o755))
				require.NoError(t, os.Symlink(outside, filepath.Join(ws, setup.link)))
				res, err := Install(context.Background(), skillIn(t, "demo"), []string{setup.agent}, tg)
				require.Error(t, err)
				var pe *PolicyError
				require.ErrorAs(t, err, &pe)
				require.Equal(t, StatusFailed, res.Agents[0].Status)
				entries, rerr := os.ReadDir(outside)
				require.NoError(t, rerr)
				require.Empty(t, entries, "nothing may be written outside the workspace")
			})
		}
	}
}

func TestInstallAllowsSymlinkedAncestorsInsideTheWorkspace(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "real-claude"), 0o755))
	require.NoError(t, os.Symlink("real-claude", filepath.Join(ws, ".claude")))
	_, err := Install(context.Background(), skillIn(t, "demo"), []string{"claude-code"}, tg)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(ws, "real-claude/skills/demo/SKILL.md"))
}

func TestInstallErrorsAreTyped(t *testing.T) {
	_, err := Install(context.Background(), Skill{Name: "x"}, []string{"codex"}, Target{})
	require.Error(t, err)
	tg, _ := target(t, "bogus")
	_, err = Install(context.Background(), skillIn(t, "demo"), []string{"codex"}, tg)
	require.Error(t, err)
	require.False(t, errors.Is(err, context.Canceled))
}
