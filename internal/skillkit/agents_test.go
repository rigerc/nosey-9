package skillkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAgentTableMatches176 compares every built-in agent with the table extracted
// from skills@1.7.6 (testdata/agents.1.7.6.tsv). This also covers the rows the
// skills-go table got wrong: 36 missing agents, kimi-cli, droid, kilo and pi.
// agentRow is the part of an Agent that the upstream table defines.
type agentRow struct {
	Name, SkillsDir           string
	Hidden, CreateDir, Custom bool
}

func TestAgentTableMatches176(t *testing.T) {
	data, err := os.ReadFile("testdata/agents.1.7.6.tsv")
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 79)
	table, _ := testTable(t)
	require.Len(t, table.IDs(), 79)
	for _, line := range lines {
		cols := strings.Split(line, "\t")
		require.GreaterOrEqual(t, len(cols), 3, line)
		a, ok := table.Get(cols[0])
		require.True(t, ok, "missing agent %s", cols[0])
		flags := ""
		if len(cols) > 3 {
			flags = cols[3]
		}
		want := agentRow{Name: cols[1], SkillsDir: cols[2], Hidden: strings.Contains(flags, "showInUniversalList=false"), CreateDir: strings.Contains(flags, "createProjectSkillsDirByDefault=true")}
		require.Equal(t, want, agentRow{Name: a.Name, SkillsDir: a.SkillsDir, Hidden: a.hiddenFromUniversal, CreateDir: a.createProjectDir, Custom: a.Custom}, cols[0])
	}
	// Rows skills-go got wrong.
	for id, dir := range map[string]string{"droid": ".agents/skills", "kilo": ".agents/skills", "pi": ".agents/skills", "kimi-code-cli": ".agents/skills", "codex": ".agents/skills"} {
		a, ok := table.Get(id)
		require.True(t, ok, id)
		require.Equal(t, dir, a.SkillsDir, id)
	}
	_, old := table.Get("kimi-cli")
	require.False(t, old)
}

func TestAgentTableIsSortedAndSharesDirs(t *testing.T) {
	table, _ := testTable(t)
	ids := table.IDs()
	require.IsNonDecreasing(t, ids)
	dirs := table.SkillsDirs()
	require.IsNonDecreasing(t, dirs)
	require.Contains(t, dirs, ".agents/skills")
	require.NotContains(t, table.UniversalIDs(), "universal")
	require.NotContains(t, table.UniversalIDs(), "replit")
	require.Contains(t, table.UniversalIDs(), "codex")
}

func TestCustomAgents(t *testing.T) {
	table, _ := testTable(t, CustomAgent{ID: "my-agent", Name: "My Agent", SkillsDir: ".my-agent/skills"})
	a, ok := table.Get("my-agent")
	require.True(t, ok)
	require.True(t, a.Custom)
	require.Equal(t, ".my-agent/skills", a.SkillsDir)
	require.Contains(t, table.IDs(), "my-agent")
	require.Len(t, table.IDs(), 80)

	bad := []CustomAgent{
		{ID: "claude-code", Name: "X", SkillsDir: ".x/skills"},
		{ID: "Bad ID", Name: "X", SkillsDir: ".x/skills"},
		{ID: "../up", Name: "X", SkillsDir: ".x/skills"},
		{ID: "ok", Name: "", SkillsDir: ".x/skills"},
		{ID: "ok", Name: "X", SkillsDir: "/abs/skills"},
		{ID: "ok", Name: "X", SkillsDir: "../outside"},
		{ID: "ok", Name: "X", SkillsDir: "a/../../outside"},
		{ID: "ok", Name: "X", SkillsDir: "."},
		{ID: "ok", Name: "X", SkillsDir: ""},
		{ID: "ok", Name: "X", SkillsDir: `a\b`},
		{ID: "ok", Name: "X", SkillsDir: ".git/hooks"},
	}
	for _, c := range bad {
		_, err := NewAgentTable(t.TempDir(), []CustomAgent{c})
		require.Error(t, err, "%+v", c)
	}
	_, err := NewAgentTable(t.TempDir(), []CustomAgent{{ID: "dup", Name: "A", SkillsDir: ".a"}, {ID: "dup", Name: "B", SkillsDir: ".b"}})
	require.Error(t, err, "duplicate custom IDs")
}

func TestDetectedAgents(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	env := map[string]string{}
	table, err := NewAgentTable(home, nil)
	require.NoError(t, err)
	table = table.WithEnv(func(k string) string { return env[k] })
	require.Empty(t, table.Detected(ws))

	mk := func(base, rel string) {
		require.NoError(t, os.MkdirAll(filepath.Join(base, rel), 0o750))
	}
	mk(home, ".codex")              // codex via default home
	mk(home, ".config/opencode")    // XDG default config home
	mk(ws, ".replit")               // workspace-relative
	mk(home, ".gemini/antigravity") // nested
	mk(home, ".openclaw")           // any-of
	require.NoError(t, os.WriteFile(filepath.Join(ws, "package.json"), []byte(`{"devDependencies":{"eve":"1"}}`), 0o600))
	mk(ws, "agent")
	require.Equal(t, []string{"antigravity", "codex", "eve", "gemini-cli", "openclaw", "opencode", "replit"}, table.Detected(ws))

	// Environment overrides.
	other := t.TempDir()
	env["CODEX_HOME"] = filepath.Join(other, "nothing")
	env["XDG_CONFIG_HOME"] = filepath.Join(other, "xdg")
	mk(other, "xdg/goose")
	got := table.Detected(ws)
	require.NotContains(t, got, "codex")
	require.NotContains(t, got, "opencode")
	require.Contains(t, got, "goose")
	require.NotContains(t, table.Detected(""), "eve", "workspace-relative detection needs a workspace")
}
