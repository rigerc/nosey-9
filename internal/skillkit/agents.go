package skillkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// UniversalSkillsDir is the project directory shared by "universal" agents and
// used as the canonical copy for symlink installs.
const UniversalSkillsDir = ".agents/skills"

// Agent describes where an agent reads project skills.
type Agent struct {
	// ID is the stable identifier used in settings and on the command line.
	ID string
	// Name is the display name.
	Name string
	// SkillsDir is the slash-separated project-relative skills directory.
	SkillsDir string
	// Custom marks agents supplied by the user rather than the built-in table.
	Custom bool

	hiddenFromUniversal bool
	createProjectDir    bool
	detect              func(detectEnv) bool
}

// Universal reports whether the agent reads the shared .agents/skills directory.
func (a Agent) Universal() bool { return a.SkillsDir == UniversalSkillsDir }

// CustomAgent is a user-defined agent. SkillsDir must be a workspace-relative path.
type CustomAgent struct {
	ID        string
	Name      string
	SkillsDir string
}

var customAgentID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// AgentTable is the agent set shared by discovery, install, list and uninstall.
// The zero value is empty; build one with NewAgentTable.
type AgentTable struct {
	agents map[string]Agent
	ids    []string
	home   string
	getenv func(string) string
	goos   string
}

// NewAgentTable returns the built-in agents plus the custom ones. An empty home
// falls back to the current user's home directory. Custom agents need a safe
// unique ID, a name and a workspace-local skills directory, and may not reuse a
// built-in ID. Violations are returned as errors.
func NewAgentTable(home string, custom []CustomAgent) (AgentTable, error) {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	t := AgentTable{agents: make(map[string]Agent, len(builtinAgents)+len(custom)), home: home, getenv: os.Getenv, goos: goos()}
	for _, a := range builtinAgents {
		t.agents[a.ID] = a
	}
	for _, c := range custom {
		if err := validateCustomAgent(c); err != nil {
			return AgentTable{}, err
		}
		if _, dup := t.agents[c.ID]; dup {
			return AgentTable{}, fmt.Errorf("custom agent ID %q duplicates another agent", c.ID)
		}
		t.agents[c.ID] = Agent{ID: c.ID, Name: c.Name, SkillsDir: path.Clean(filepath.ToSlash(c.SkillsDir)), Custom: true}
	}
	t.ids = make([]string, 0, len(t.agents))
	for id := range t.agents {
		t.ids = append(t.ids, id)
	}
	slices.Sort(t.ids)
	return t, nil
}

func validateCustomAgent(c CustomAgent) error {
	switch {
	case !customAgentID.MatchString(c.ID):
		return fmt.Errorf("custom agent ID %q must match %s", c.ID, customAgentID)
	case strings.TrimSpace(c.Name) == "" || strings.ContainsFunc(c.Name, func(r rune) bool { return r < 32 || r >= 127 && r <= 159 }):
		return fmt.Errorf("custom agent %s needs a printable name", c.ID)
	}
	dir := c.SkillsDir
	if !filepath.IsLocal(dir) || strings.ContainsAny(dir, "\\\x00") || path.Clean(dir) == "." || path.IsAbs(dir) ||
		strings.ContainsFunc(dir, func(r rune) bool { return r < 32 || r >= 127 && r <= 159 }) {
		return fmt.Errorf("custom agent %s skillsDir must be a relative path inside the workspace", c.ID)
	}
	if first, _, _ := strings.Cut(path.Clean(dir), "/"); first == ".git" {
		return fmt.Errorf("custom agent %s skillsDir must not be inside .git", c.ID)
	}
	return nil
}

// WithEnv returns a copy of the table whose host detection reads environment
// variables through getenv. It exists so detection is testable without touching
// the process environment.
func (t AgentTable) WithEnv(getenv func(string) string) AgentTable {
	t.getenv = getenv
	return t
}

// IDs lists every agent ID in sorted order.
func (t AgentTable) IDs() []string { return slices.Clone(t.ids) }

// Get returns the agent with the given ID.
func (t AgentTable) Get(id string) (Agent, bool) {
	a, ok := t.agents[id]
	return a, ok
}

// All lists agents sorted by ID.
func (t AgentTable) All() []Agent {
	out := make([]Agent, 0, len(t.ids))
	for _, id := range t.ids {
		out = append(out, t.agents[id])
	}
	return out
}

// SkillsDirs returns the distinct skills directories of all agents in sorted order.
func (t AgentTable) SkillsDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, id := range t.ids {
		if d := t.agents[id].SkillsDir; !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	slices.Sort(dirs)
	return dirs
}

// Detected lists IDs of agents found on this host (or in workspace) in sorted
// order. Custom agents have no host marker and are never reported.
func (t AgentTable) Detected(workspace string) []string {
	env := newDetectEnv(t, workspace)
	var out []string
	for _, id := range t.ids {
		if a := t.agents[id]; a.detect != nil && a.detect(env) {
			out = append(out, id)
		}
	}
	return out
}

// UniversalIDs lists agents that share .agents/skills and are shown as universal
// (excludes the pseudo "universal" agent and replit), as upstream's
// getUniversalAgents does.
func (t AgentTable) UniversalIDs() []string {
	var out []string
	for _, id := range t.ids {
		if a := t.agents[id]; a.Universal() && !a.hiddenFromUniversal {
			out = append(out, id)
		}
	}
	return out
}

// MarshalJSON renders the table for diagnostics and golden comparison.
func (t AgentTable) MarshalJSON() ([]byte, error) { return json.Marshal(t.All()) }

// MarshalJSON exposes the stable fields of an agent.
func (a Agent) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		SkillsDir string `json:"skillsDir"`
		Custom    bool   `json:"custom,omitempty"`
	}{a.ID, a.Name, a.SkillsDir, a.Custom})
}

// detectEnv is what host detection may look at.
type detectEnv struct {
	home, configHome, workspace string
	getenv                      func(string) string
	goos                        string
}

func newDetectEnv(t AgentTable, workspace string) detectEnv {
	getenv := t.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(t.home, ".config")
	}
	return detectEnv{home: t.home, configHome: cfg, workspace: workspace, getenv: getenv, goos: t.goos}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func inHome(rel string) func(detectEnv) bool {
	return func(e detectEnv) bool { return e.home != "" && exists(filepath.Join(e.home, filepath.FromSlash(rel))) }
}
func inConfig(rel string) func(detectEnv) bool {
	return func(e detectEnv) bool { return exists(filepath.Join(e.configHome, filepath.FromSlash(rel))) }
}
func inWorkspace(rel string) func(detectEnv) bool {
	return func(e detectEnv) bool {
		return e.workspace != "" && exists(filepath.Join(e.workspace, filepath.FromSlash(rel)))
	}
}
func atPath(abs string) func(detectEnv) bool { return func(detectEnv) bool { return exists(abs) } }

// inEnvHome checks $name when set, else ~/defaultRel (trimmed like upstream).
func inEnvHome(name, defaultRel string) func(detectEnv) bool {
	return func(e detectEnv) bool {
		if v := strings.TrimSpace(e.getenv(name)); v != "" {
			return exists(v)
		}
		return e.home != "" && exists(filepath.Join(e.home, defaultRel))
	}
}

func anyOf(checks ...func(detectEnv) bool) func(detectEnv) bool {
	return func(e detectEnv) bool {
		for _, c := range checks {
			if c(e) {
				return true
			}
		}
		return false
	}
}

func never(detectEnv) bool { return false }

func reasonixHome(e detectEnv) bool {
	if v := strings.TrimSpace(e.getenv("REASONIX_HOME")); v != "" {
		return exists(v)
	}
	if e.goos == "windows" {
		appData := strings.TrimSpace(e.getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(e.home, "AppData", "Roaming")
		}
		return exists(filepath.Join(appData, "reasonix"))
	}
	return e.home != "" && exists(filepath.Join(e.home, ".reasonix"))
}

func zedConfig(e detectEnv) bool {
	if exists(filepath.Join(e.configHome, "zed")) {
		return true
	}
	if v := strings.TrimSpace(e.getenv("APPDATA")); v != "" && exists(filepath.Join(v, "Zed")) {
		return true
	}
	v := strings.TrimSpace(e.getenv("FLATPAK_XDG_CONFIG_HOME"))
	return v != "" && exists(filepath.Join(v, "zed"))
}

// eveProject mirrors upstream: an `agent` directory plus an `eve` dependency in package.json.
func eveProject(e detectEnv) bool {
	if e.workspace == "" || !exists(filepath.Join(e.workspace, "agent")) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(e.workspace, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	_, a := pkg.Dependencies["eve"]
	_, b := pkg.DevDependencies["eve"]
	return a || b
}
