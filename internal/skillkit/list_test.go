package skillkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func listTarget(t *testing.T, custom ...CustomAgent) (Target, string, string) {
	t.Helper()
	home := t.TempDir()
	table, err := NewAgentTable(home, custom)
	require.NoError(t, err)
	table = table.WithEnv(func(string) string { return "" })
	ws := t.TempDir()
	return Target{Workspace: ws, Mode: ModeSymlink, Agents: table}, ws, home
}

// TestListCodexOnlyInstallListsCodex is the regression for skills-go listing no
// agents for a Codex-only install, which turned every status into "unknown".
func TestListCodexOnlyInstallListsCodex(t *testing.T) {
	tg, _, home := listTarget(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	_, err := Install(context.Background(), skillIn(t, "demo"), []string{"codex"}, tg)
	require.NoError(t, err)
	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Len(t, inv.Skills, 1)
	require.Equal(t, "demo", inv.Skills[0].Name)
	require.Equal(t, []string{"codex"}, inv.Skills[0].Agents)
	require.Equal(t, ".agents/skills/demo", inv.Skills[0].Path)
	require.Empty(t, inv.Warnings)
}

func TestListAttributesSharedDirectoryOnlyToDetectedUniversalAgents(t *testing.T) {
	tg, _, home := listTarget(t, CustomAgent{ID: "shared-custom", Name: "S", SkillsDir: ".agents/skills"})
	for _, d := range []string{".codex", ".cursor"} { // detected; gemini-cli and others are not
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	_, err := Install(context.Background(), skillIn(t, "demo"), []string{"codex"}, tg)
	require.NoError(t, err)
	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Equal(t, []string{"codex", "cursor", "shared-custom"}, inv.Skills[0].Agents)
}

func TestListAttributesAgentSpecificDirectoriesDirectly(t *testing.T) {
	tg, ws, _ := listTarget(t, CustomAgent{ID: "my-agent", Name: "M", SkillsDir: ".my/skills"})
	tg.CreateMissingAgentRoot = true
	_, err := Install(context.Background(), skillIn(t, "demo"), []string{"claude-code", "windsurf", "my-agent"}, tg)
	require.NoError(t, err)
	// Nothing is detected on this host, yet agent-specific directories still attribute.
	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Len(t, inv.Skills, 1)
	require.Equal(t, []string{"claude-code", "my-agent", "windsurf"}, inv.Skills[0].Agents)

	// A copy install has no canonical directory but is still listed.
	require.NoError(t, os.RemoveAll(filepath.Join(ws, ".agents")))
	require.NoError(t, os.RemoveAll(filepath.Join(ws, ".claude")))
	tg.Mode = ModeCopy
	_, err = Install(context.Background(), skillIn(t, "demo"), []string{"claude-code"}, tg)
	require.NoError(t, err)
	inv, err = ListInstalled(tg)
	require.NoError(t, err)
	require.Len(t, inv.Skills, 1)
	require.Equal(t, []string{"claude-code"}, inv.Skills[0].Agents)
}

func TestListSharedNonUniversalDirectoryPrefersDetectedAgents(t *testing.T) {
	tg, ws, home := listTarget(t)
	dir := filepath.Join(ws, ".qoder/skills/demo")
	writeSkill(t, dir, "demo", nil)
	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Equal(t, []string{"qoder"}, inv.Skills[0].Agents, "undetected: first agent by ID")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".qoder-cn"), 0o755))
	inv, err = ListInstalled(tg)
	require.NoError(t, err)
	require.Equal(t, []string{"qoder-cn"}, inv.Skills[0].Agents)
}

func TestListSurfacesParseErrorsAndSkipsOutsideDirectories(t *testing.T) {
	tg, ws, _ := listTarget(t)
	writeSkill(t, filepath.Join(ws, ".agents/skills/good"), "good", nil)
	bad := filepath.Join(ws, ".agents/skills/bad")
	require.NoError(t, os.MkdirAll(bad, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("garbage"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".agents/skills/.nosey-tmp-leftover"), 0o755))
	outside := t.TempDir()
	writeSkill(t, filepath.Join(outside, "skills/stolen"), "stolen", nil)
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, ".claude")))
	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Len(t, inv.Skills, 1)
	require.Equal(t, "good", inv.Skills[0].Name)
	codes := map[string]bool{}
	for _, w := range inv.Warnings {
		codes[w.Code] = true
	}
	require.True(t, codes[WarnParseError])
	require.True(t, codes[WarnOutsideRoot])
}

func TestListEmptyWorkspace(t *testing.T) {
	tg, _, _ := listTarget(t)
	inv, err := ListInstalled(tg)
	require.NoError(t, err)
	require.Empty(t, inv.Skills)
}
