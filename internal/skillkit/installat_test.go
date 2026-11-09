package skillkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func sourceSkill(t *testing.T, name, body string) Skill {
	t.Helper()
	dir := writeSkill(t, filepath.Join(t.TempDir(), "src", name), name, map[string]string{"data.txt": body})
	return mustDiscover(t, dir, DiscoverOptions{})[0]
}

func TestListInstalledReportsLocationsAndModes(t *testing.T) {
	tg, ws := target(t, ModeSymlink, CustomAgent{ID: "mine", Name: "Mine", SkillsDir: ".mine/skills"})
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".claude"), 0o755))
	_, err := Install(context.Background(), skillIn(t, "demo"), []string{"claude-code"}, tg)
	require.NoError(t, err)
	tg.Mode = ModeCopy
	_, err = Install(context.Background(), skillIn(t, "demo"), []string{"mine"}, tg)
	require.NoError(t, err)

	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Len(t, inv.Skills, 1)
	got := map[string]Location{}
	for _, l := range inv.Skills[0].Locations {
		got[l.Path] = l
	}
	require.Len(t, got, 3)
	require.Equal(t, UniversalSkillsDir, inv.Skills[0].Locations[0].Dir, "canonical first")
	require.False(t, got[".agents/skills/demo"].Symlink)
	require.Empty(t, got[".agents/skills/demo"].Agents, "no universal agent is detected, but the location is still reported")
	require.True(t, got[".claude/skills/demo"].Symlink)
	require.Equal(t, []string{"claude-code"}, got[".claude/skills/demo"].Agents)
	require.False(t, got[".mine/skills/demo"].Symlink)
}

func TestInstallAtRefreshesEachLocationAndKeepsItsMode(t *testing.T) {
	tg, ws := target(t, ModeSymlink, CustomAgent{ID: "mine", Name: "Mine", SkillsDir: ".mine/skills"})
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".claude"), 0o755))
	_, err := Install(context.Background(), sourceSkill(t, "demo", "v1"), []string{"claude-code"}, tg)
	require.NoError(t, err)
	tg.Mode = ModeCopy
	_, err = Install(context.Background(), sourceSkill(t, "demo", "v1"), []string{"mine"}, tg)
	require.NoError(t, err)
	inv, err := ListInstalled(tg)
	require.NoError(t, err)

	res, err := InstallAt(context.Background(), sourceSkill(t, "demo", "v2"), inv.Skills[0].Locations, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Status)
	require.Len(t, res.Agents, 3)
	require.NotEmpty(t, res.Hash)
	for _, rel := range []string{".agents/skills/demo", ".claude/skills/demo", ".mine/skills/demo"} {
		require.Equal(t, "v2", readFile(t, filepath.Join(ws, rel, "data.txt")), rel)
	}
	info, err := os.Lstat(filepath.Join(ws, ".claude/skills/demo"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "symlink location stays a symlink")
	info, err = os.Lstat(filepath.Join(ws, ".mine/skills/demo"))
	require.NoError(t, err)
	require.Zero(t, info.Mode()&os.ModeSymlink, "copy location stays a copy")
	require.NoDirExists(t, filepath.Join(ws, ".windsurf"), "no location is added")
}

func TestInstallAtCreatesSymlinkAndCanonicalForNewLocations(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	locs := []Location{
		{Dir: ".claude/skills", Path: ".claude/skills/demo", Symlink: true},
		{Dir: ".windsurf/skills", Path: ".windsurf/skills/demo"},
	}
	res, err := InstallAt(context.Background(), sourceSkill(t, "demo", "v1"), locs, tg)
	require.NoError(t, err)
	require.Equal(t, StatusInstalled, res.Status)
	require.Equal(t, "v1", readFile(t, filepath.Join(ws, ".agents/skills/demo/data.txt")), "the canonical copy is provided for symlinks")
	target, err := os.Readlink(filepath.Join(ws, ".claude/skills/demo"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join("..", "..", ".agents", "skills", "demo"), target)
	require.Equal(t, "v1", readFile(t, filepath.Join(ws, ".windsurf/skills/demo/data.txt")))
}

func TestInstallAtRefusesUnknownDirectoriesAndEscapingAncestors(t *testing.T) {
	tg, ws := target(t, ModeSymlink)
	_, err := InstallAt(context.Background(), sourceSkill(t, "demo", "v1"), []Location{{Dir: "evil", Path: "evil/demo"}}, tg)
	var policy *PolicyError
	require.ErrorAs(t, err, &policy)
	require.NoDirExists(t, filepath.Join(ws, "evil"))

	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, ".claude")))
	res, err := InstallAt(context.Background(), sourceSkill(t, "demo", "v1"), []Location{{Dir: ".claude/skills", Path: ".claude/skills/demo"}}, tg)
	require.ErrorAs(t, err, &policy)
	require.Equal(t, StatusFailed, res.Status)
	require.NoDirExists(t, filepath.Join(outside, "skills"))

	_, err = InstallAt(context.Background(), sourceSkill(t, "demo", "v1"), nil, tg)
	require.Error(t, err)
}

func TestInstallAtStopsWhenCancelled(t *testing.T) {
	tg, ws := target(t, ModeCopy)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := InstallAt(ctx, sourceSkill(t, "demo", "v1"), []Location{{Dir: UniversalSkillsDir, Path: ".agents/skills/demo"}}, tg)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, StatusNotStarted, res.Status)
	require.NoDirExists(t, filepath.Join(ws, ".agents"))
}
