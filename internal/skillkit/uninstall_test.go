package skillkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func installed(t *testing.T, mode Mode, agents ...string) (Target, string) {
	t.Helper()
	tg, ws := target(t, mode, CustomAgent{ID: "my-agent", Name: "M", SkillsDir: ".my/skills"})
	tg.CreateMissingAgentRoot = true
	_, err := Install(context.Background(), skillIn(t, "demo"), agents, tg)
	require.NoError(t, err)
	return tg, ws
}

func TestUninstallRemovesAgentDirsBeforeCanonical(t *testing.T) {
	tg, ws := installed(t, ModeSymlink, "claude-code", "codex", "my-agent", "windsurf")
	res, err := Uninstall(context.Background(), "demo", tg)
	require.NoError(t, err)
	removed := res.Removed()
	require.Contains(t, removed, ".claude/skills/demo")
	require.Contains(t, removed, ".my/skills/demo")
	require.Contains(t, removed, ".windsurf/skills/demo")
	require.Equal(t, ".agents/skills/demo", removed[len(removed)-1], "canonical directory goes last")
	for _, p := range removed {
		_, lerr := os.Lstat(filepath.Join(ws, p))
		require.True(t, os.IsNotExist(lerr), p)
	}
}

func TestUninstallCopiesAndAbsentPaths(t *testing.T) {
	tg, ws := installed(t, ModeCopy, "claude-code", "codex")
	res, err := Uninstall(context.Background(), "demo", tg)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{".claude/skills/demo", ".agents/skills/demo"}, res.Removed())
	again, err := Uninstall(context.Background(), "demo", tg)
	require.NoError(t, err, "uninstalling twice is not an error")
	require.Empty(t, again.Removed())
	require.NoDirExists(t, filepath.Join(ws, ".claude/skills/demo"))
}

func TestUninstallSharedCanonicalDirectoryOnlyOnce(t *testing.T) {
	tg, _ := installed(t, ModeSymlink, "codex", "cursor", "gemini-cli", "amp")
	res, err := Uninstall(context.Background(), "demo", tg)
	require.NoError(t, err)
	require.Equal(t, []string{".agents/skills/demo"}, res.Removed())
}

func TestUninstallRejectsUnsafeNamesFromTheLock(t *testing.T) {
	tg, ws := installed(t, ModeSymlink, "claude-code")
	victim := filepath.Join(ws, "victim")
	require.NoError(t, os.MkdirAll(victim, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(victim, "keep"), []byte("x"), 0o644))
	for _, name := range []string{"", ".", "..", "../victim", "../../etc", "a/b", `a\b`, "/etc/passwd", "bad\x00name", "new\nline"} {
		_, err := Uninstall(context.Background(), name, tg)
		var in *InvalidNameError
		require.ErrorAs(t, err, &in, "%q", name)
	}
	require.FileExists(t, filepath.Join(victim, "keep"))
	require.DirExists(t, filepath.Join(ws, ".agents/skills/demo"))
}

func TestUninstallNeverFollowsSymlinks(t *testing.T) {
	tg, ws := installed(t, ModeCopy, "codex")
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "precious"), []byte("keep"), 0o644))
	// An agent directory entry that is a symlink to an outside directory is unlinked, not followed.
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".claude/skills"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, ".claude/skills/demo")))
	// A symlink inside the canonical directory is removed with it, not followed.
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, ".agents/skills/demo/outside-link")))
	res, err := Uninstall(context.Background(), "demo", tg)
	require.NoError(t, err)
	var sawLink bool
	for _, r := range res.Removals {
		if r.Path == ".claude/skills/demo" {
			sawLink = r.Symlink && r.Removed
		}
	}
	require.True(t, sawLink)
	require.FileExists(t, filepath.Join(outside, "precious"))
	_, err = os.Lstat(filepath.Join(ws, ".claude/skills/demo"))
	require.True(t, os.IsNotExist(err))
}

func TestUninstallRefusesAncestorsThatLeaveTheWorkspace(t *testing.T) {
	for name, custom := range map[string]string{"built-in": ".claude", "custom": ".my"} {
		t.Run(name, func(t *testing.T) {
			tg, ws := installed(t, ModeCopy, "codex")
			outside := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(outside, "skills/demo"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(outside, "skills/demo/keep"), []byte("x"), 0o644))
			require.NoError(t, os.Symlink(outside, filepath.Join(ws, custom)))
			res, err := Uninstall(context.Background(), "demo", tg)
			var pe *PolicyError
			require.ErrorAs(t, err, &pe)
			require.FileExists(t, filepath.Join(outside, "skills/demo/keep"))
			require.DirExists(t, filepath.Join(ws, ".agents/skills/demo"), "canonical is kept while an agent path could not be handled")
			require.NotEmpty(t, res.Removals)
		})
	}
}

func TestUninstallReturnsEveryError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission errors need a non-root user")
	}
	tg, ws := installed(t, ModeCopy, "claude-code", "windsurf", "codex")
	for _, d := range []string{".claude/skills", ".windsurf/skills"} {
		require.NoError(t, os.Chmod(filepath.Join(ws, d), 0o500))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(ws, d), 0o755) })
	}
	res, err := Uninstall(context.Background(), "demo", tg)
	require.Error(t, err)
	var failed []string
	for _, r := range res.Removals {
		if r.Err != nil && !r.Skipped {
			failed = append(failed, r.Path)
		}
	}
	require.ElementsMatch(t, []string{".claude/skills/demo", ".windsurf/skills/demo"}, failed, "both failures are reported")
	require.Contains(t, err.Error(), ".claude/skills/demo")
	require.Contains(t, err.Error(), ".windsurf/skills/demo")
	require.DirExists(t, filepath.Join(ws, ".agents/skills/demo"), "canonical kept")
}

func TestUninstallHonoursCancellation(t *testing.T) {
	tg, ws := installed(t, ModeCopy, "claude-code")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Uninstall(ctx, "demo", tg)
	require.ErrorIs(t, err, context.Canceled)
	require.DirExists(t, filepath.Join(ws, ".claude/skills/demo"))
}
