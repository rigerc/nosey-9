// Package detection evaluates bounded workspace observations with explicit unknown states.
package detection

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/dlclark/regexp2"
	"github.com/pelletier/go-toml/v2"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Result contains technology states, positive evidence and execution coverage.
type Result struct {
	Detections   []domain.Detection
	States       map[string]string
	Coverage     domain.Coverage
	Capabilities domain.Capabilities
	Diagnostics  []domain.Diagnostic
}
type file struct {
	directory bool
	unsafe    bool
	path      string
	data      []byte
	err       error
}
type observation struct{ ecosystem, name, path string }
type engine struct {
	projects       []string
	scope          string
	ctx            context.Context
	root           string
	settings       settings.Settings
	files          []file
	observations   []observation
	patterns       map[string]*regexp2.Regexp
	truncated      bool
	providerErrors map[string]bool
}

var kinds = []string{"file", "dependency", "content", "extension", "dotenv", "detector", "mcp_package"}
var ecosystems = []string{"npm", "python", "cargo", "go", "ruby", "composer", "docker", "terraform", "terraform.resource", "githubAction", "deno", "hex"}

// Scan evaluates effective rules inside the exact workspace root.
func Scan(ctx context.Context, root string, c domain.Catalog, cfg settings.Settings) (Result, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	e := engine{ctx: ctx, root: root, settings: cfg, patterns: map[string]*regexp2.Regexp{}, providerErrors: map[string]bool{}}
	if err := e.walk(); err != nil {
		if ctx.Err() != nil && parent.Err() == nil {
			e.truncated = true
			if len(e.projects) == 0 {
				e.projects = []string{"."}
			}
		} else {
			return Result{}, err
		}
	}
	e.observe()
	slices.SortFunc(e.observations, func(a, b observation) int {
		if a.ecosystem != b.ecosystem {
			return strings.Compare(a.ecosystem, b.ecosystem)
		}
		if a.path != b.path {
			return strings.Compare(a.path, b.path)
		}
		return strings.Compare(a.name, b.name)
	})
	e.observations = slices.Compact(e.observations)
	r := Result{Detections: []domain.Detection{}, States: map[string]string{}, Diagnostics: []domain.Diagnostic{}, Coverage: domain.Coverage{Unknown: []string{}, Gaps: []domain.CoverageGap{}}, Capabilities: domain.Capabilities{SupportedKinds: slices.Clone(kinds), Ecosystems: slices.Clone(ecosystems), UnsupportedKinds: []string{}, UnavailableDetectors: []string{}, MCP: cfg.Detection.MCP, Versions: []int{1, 2}}}
	ids := []string{}
	for id := range c.Technologies {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		t := c.Technologies[id]
		state := "absent"
		evidence := []domain.Evidence{}
		active := 0
		for _, rule := range t.Rules {
			for _, project := range e.projects {
				e.scope = project
				if rule.Disabled {
					continue
				}
				active++
				if (rule.Kind == "mcp_package" && !cfg.Detection.MCP) || (rule.Kind == "detector" && !e.adapterEnabled(rule.Detector)) {
					r.Coverage.Excluded++
					r.Coverage.Gaps = append(r.Coverage.Gaps, domain.CoverageGap{TechnologyID: id, RuleID: rule.ID, Kind: rule.Kind, Reason: "excluded-by-mode", ProjectPath: project})
					state = "unknown"
					continue
				}
				r.Coverage.Required++
				matched, err := e.evaluate(rule)
				if err != nil || e.truncated {
					r.Coverage.Unevaluated++
					reason := "execution-error"
					if e.truncated {
						reason = "budget-exceeded"
					}
					r.Coverage.Gaps = append(r.Coverage.Gaps, domain.CoverageGap{TechnologyID: id, RuleID: rule.ID, Kind: rule.Kind, Reason: reason, Provider: rule.Ecosystem, ProjectPath: project})
					state = "unknown"
				} else {
					r.Coverage.Evaluated++
				}
				evidence = append(evidence, matched...)
			}
		}
		if len(evidence) > 0 {
			state = "detected"
			r.Detections = append(r.Detections, domain.Detection{ID: id, Evidence: evidence})
		} else if active == 0 {
			state = "unknown"
		}
		r.States[id] = state
		if state == "unknown" {
			r.Coverage.Unknown = append(r.Coverage.Unknown, id)
		}
	}
	for id := range c.InvalidTechnologies {
		r.States[id] = "unknown"
		r.Coverage.Unknown = append(r.Coverage.Unknown, id)
		r.Coverage.Required++
		r.Coverage.Unevaluated++
		r.Coverage.Gaps = append(r.Coverage.Gaps, domain.CoverageGap{TechnologyID: id, RuleID: "invalid-effective-definition", Kind: "detector", Reason: "other", ProjectPath: "."})
	}
	slices.Sort(r.Coverage.Unknown)
	r.Coverage.UnknownCount = len(r.Coverage.Unknown)
	r.Capabilities.UnavailableRules = r.Coverage.Unevaluated
	return r, nil
}
func (e *engine) excluded(path string) bool {
	for _, pattern := range e.settings.Detection.Exclude {
		ok, _ := doublestar.PathMatch(pattern, path)
		dir, _ := doublestar.PathMatch(pattern, path+"/")
		if ok || dir {
			return true
		}
	}
	return false
}
func (e *engine) walk() error {
	out, err := (process.Runner{Timeout: 5 * time.Second, Limit: 4 << 20}).Run(e.ctx, e.root, nil, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ".")
	names := []string{}
	if err == nil {
		for _, name := range strings.Split(string(out.Stdout), "\x00") {
			if name != "" && filepath.IsLocal(name) {
				names = append(names, filepath.ToSlash(name))
			}
		}
	} else {
		err = filepath.WalkDir(e.root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				e.truncated = true
				return nil
			}
			if e.ctx.Err() != nil {
				return e.ctx.Err()
			}
			rel, relErr := filepath.Rel(e.root, path)
			if relErr != nil {
				return relErr
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if strings.Count(rel, "/") > 64 || e.excluded(rel) || entry.Name() == ".git" {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if _, statErr := os.Lstat(filepath.Join(path, ".git")); statErr == nil {
					return filepath.SkipDir
				}
				return nil
			}
			names = append(names, rel)
			if len(names) > 20000 {
				e.truncated = true
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	// Narrow explicit inspection of ignored secret-config files; values are never evidence.
	projects := map[string]bool{".": true}
	for _, name := range names {
		base := filepath.Base(name)
		if slices.Contains([]string{"package.json", "go.mod", "pyproject.toml", "Cargo.toml", "Gemfile", "composer.json", "deno.json", "deno.jsonc", "mix.exs"}, base) {
			projects[filepath.ToSlash(filepath.Dir(name))] = true
		}
	}
	special := []string{".env", ".env.local", ".env.development", ".env.production"}
	if e.settings.Detection.MCP {
		special = append(special, ".mcp.json", ".cursor/mcp.json", ".vscode/mcp.json", ".codex/config.toml")
	}
	for project := range projects {
		e.projects = append(e.projects, project)
	}
	slices.Sort(e.projects)
	for project := range projects {
		entries, readErr := os.ReadDir(filepath.Join(e.root, project))
		if readErr != nil {
			e.truncated = true
		}
		for _, entry := range entries {
			if !entry.IsDir() && (entry.Name() == ".env" || strings.HasPrefix(entry.Name(), ".env.")) {
				names = append(names, filepath.ToSlash(filepath.Join(project, entry.Name())))
			}
		}
		for _, name := range special {
			rel := filepath.ToSlash(filepath.Join(project, name))
			if _, statErr := os.Lstat(filepath.Join(e.root, rel)); statErr == nil {
				names = append(names, rel)
			}
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if e.excluded(name) {
			continue
		}
		info, statErr := os.Lstat(filepath.Join(e.root, name))
		if statErr != nil || info.IsDir() {
			continue
		}
		f := file{path: name}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(filepath.Join(e.root, name))
			if err != nil {
				f.unsafe = true
			} else {
				relative, err := filepath.Rel(e.root, target)
				f.unsafe = err != nil || !filepath.IsLocal(relative)
				if stat, err := os.Stat(target); err == nil && stat.IsDir() {
					f.directory = true
					f.unsafe = true
				}
			}
		}
		f.data, f.err = storage.Read(e.root, name, e.settings.Detection.MaxBytes)
		e.files = append(e.files, f)
		if len(e.files) >= 20000 {
			e.truncated = true
			break
		}
	}
	directories := map[string]bool{}
	for _, f := range e.files {
		for parent := filepath.ToSlash(filepath.Dir(f.path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if !e.excluded(parent) {
				directories[parent] = true
			}
		}
	}
	for path := range directories {
		e.files = append(e.files, file{path: path, directory: true})
	}
	slices.SortFunc(e.files, func(a, b file) int { return strings.Compare(a.path, b.path) })
	return nil
}
func (e *engine) projectFor(path string) string {
	best := "."
	for _, project := range e.projects {
		if project != "." && (strings.HasPrefix(path, project+"/") || path == project) && (best == "." || len(project) > len(best)) {
			best = project
		}
	}
	return best
}
func (e *engine) match(pattern, value string, insensitive bool) (bool, error) {
	key := pattern
	if insensitive {
		key = "(?i)" + key
	}
	re, ok := e.patterns[key]
	if !ok {
		var err error
		re, err = regexp2.Compile(key, regexp2.ECMAScript)
		if err != nil {
			return false, err
		}
		re.MatchTimeout = 50 * time.Millisecond
		e.patterns[key] = re
	}
	return re.MatchString(value)
}
func (e *engine) evaluate(rule domain.Rule) ([]domain.Evidence, error) {
	found := []domain.Evidence{}
	add := func(path, identifier string) {
		project := e.projectFor(path)
		found = append(found, domain.Evidence{RuleID: rule.ID, Kind: rule.Kind, ProjectPath: project, Path: storage.Safe(path), Identifier: storage.Safe(identifier)})
	}
	if rule.Kind == "dependency" || rule.Kind == "mcp_package" {
		eco := rule.Ecosystem
		if rule.Kind == "mcp_package" {
			eco = "mcp"
		}

		for _, o := range e.observations {
			if e.projectFor(o.path) != e.scope {
				continue
			}
			if o.ecosystem != eco {
				continue
			}
			match := o.name == rule.Name
			if rule.NameRegex != "" {
				var err error
				match, err = e.match(rule.NameRegex, o.name, false)
				if err != nil {
					return found, err
				}
			}
			if match {
				add(o.path, o.name)
			}
		}
		if e.providerErrors[eco+"\x00"+e.scope] {
			return found, fmt.Errorf("unreadable %s manifest", eco)
		}
		return found, nil
	}
	if rule.Kind == "detector" {
		return e.runAdapter(rule)
	}
	for _, f := range e.files {
		if e.projectFor(f.path) != e.scope || f.directory && rule.Kind != "file" {
			continue
		}
		if e.ctx.Err() != nil {
			return found, e.ctx.Err()
		}
		name := f.path
		match := false
		switch rule.Kind {
		case "file":
			if rule.Glob != "" {
				var err error
				match, err = doublestar.PathMatch(rule.Glob, name)
				if err != nil {
					return found, err
				}
			} else {
				if rule.PathScope == "basename" {
					name = filepath.Base(name)
				}
				var err error
				match, err = e.match(rule.PathRegex, name, false)
				if err != nil {
					return found, err
				}
			}
		case "extension":
			match = strings.HasSuffix(name, rule.Extension)
		case "content":
			ok, err := doublestar.PathMatch(rule.Glob, name)
			if err != nil {
				return found, err
			}
			if !ok {
				continue
			}
			if f.err != nil {
				return found, f.err
			}
			if rule.Contains != "" {
				match = strings.Contains(string(f.data), rule.Contains)
			} else {
				match, err = e.match(rule.Regex, string(f.data), rule.RegexFlags == "i")
				if err != nil {
					return found, err
				}
			}
		case "dotenv":
			base := filepath.Base(name)
			if rule.Glob != "" {
				ok, err := doublestar.PathMatch(rule.Glob, name)
				if err != nil {
					return found, err
				}
				if !ok {
					continue
				}
			} else if !strings.HasPrefix(base, ".env") || strings.Contains(base, "example") || strings.Contains(base, "sample") {
				continue
			}
			if f.err != nil {
				return found, f.err
			}
			for _, line := range strings.Split(string(f.data), "\n") {
				key, _, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
				if !ok {
					continue
				}
				key = strings.TrimSpace(key)
				if !dotenvKey.MatchString(key) {
					continue
				}
				hit := key == rule.Key
				if rule.KeyRegex != "" {
					var err error
					hit, err = e.match(rule.KeyRegex, key, false)
					if err != nil {
						return found, err
					}
				}
				if hit {
					add(name, key)
				}
			}
			continue
		default:
			return found, fmt.Errorf("unsupported rule kind %s", rule.Kind)
		}
		if match {
			if f.unsafe {
				return found, fmt.Errorf("matched path is an unsafe symlink")
			}
			add(f.path, "")
		}
	}
	return found, nil
}
func (e *engine) adapterEnabled(id string) bool {
	for _, a := range e.settings.Detection.Adapters {
		if a.ID == id && a.Enabled {
			return true
		}
	}
	return false
}
func (e *engine) runAdapter(rule domain.Rule) ([]domain.Evidence, error) {
	for _, a := range e.settings.Detection.Adapters {
		if a.ID != rule.Detector || !a.Enabled {
			continue
		}
		if !filepath.IsAbs(a.Path) {
			return nil, fmt.Errorf("adapter path must be absolute")
		}
		data, err := os.ReadFile(a.Path)
		if err != nil {
			return nil, err
		}
		if storage.Hash(data) != a.Hash {
			return nil, fmt.Errorf("adapter hash mismatch")
		}
		request, err := json.Marshal(struct {
			Version        int         `json:"protocolVersion"`
			AdapterID      string      `json:"adapterId"`
			AdapterVersion string      `json:"adapterVersion"`
			Workspace      string      `json:"workspace"`
			ProjectPath    string      `json:"projectPath"`
			Rule           domain.Rule `json:"rule"`
		}{1, a.ID, a.Version, e.root, e.scope, rule})
		if err != nil {
			return nil, err
		}
		out, err := (process.Runner{Timeout: 5 * time.Second, Limit: 1 << 20}).RunInput(e.ctx, e.root, []string{"NOSEY_ADAPTER_VERSION=" + a.Version}, request, a.Path)
		if err != nil {
			return nil, err
		}
		var items []domain.Evidence
		if err = json.Unmarshal(out.Stdout, &items); err != nil {
			return nil, err
		}
		for i := range items {
			items[i].RuleID = rule.ID
			items[i].Kind = rule.Kind
			items[i].Identifier = "" // Custom adapters may not introduce arbitrary secret-bearing identifiers.
			if !filepath.IsLocal(items[i].Path) {
				return nil, fmt.Errorf("adapter evidence escapes workspace")
			}
			if _, err := storage.Read(e.root, items[i].Path, e.settings.Detection.MaxBytes); err != nil {
				return nil, fmt.Errorf("adapter evidence must identify an accessible confined file")
			}
			if e.projectFor(items[i].Path) != e.scope {
				return nil, fmt.Errorf("adapter evidence escapes project scope")
			}
			items[i].Path = storage.Safe(items[i].Path)
			items[i].ProjectPath = e.scope
		}
		return items, nil
	}
	return nil, fmt.Errorf("required adapter unavailable")
}
func (e *engine) add(ecosystem, name, path string) {
	if name != "" {
		e.observations = append(e.observations, observation{ecosystem, name, path})
	}
}

var manifestLine = regexp.MustCompile(`(?m)^\s*(?:require\s+|gem\s+)?["']?([A-Za-z0-9@_./+-]+)["']?\s*(?:[=,: ]|$)`)
var dotenvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var hexDependency = regexp.MustCompile(`\{\s*:([A-Za-z0-9_]+)\s*,`)

func (e *engine) observe() {
	for _, f := range e.files {
		if f.directory {
			continue
		}
		base := filepath.Base(f.path)
		ecosystem := ""
		switch {
		case base == "package.json":
			ecosystem = "npm"
		case base == "pyproject.toml" || strings.HasPrefix(base, "requirements") || base == "Pipfile":
			ecosystem = "python"
		case base == "Cargo.toml":
			ecosystem = "cargo"
		case base == "go.mod":
			ecosystem = "go"
		case base == "Gemfile" || base == "Gemfile.lock":
			ecosystem = "ruby"
		case base == "composer.json":
			ecosystem = "composer"
		case strings.HasPrefix(base, "Dockerfile") || slices.Contains([]string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}, base):
			ecosystem = "docker"
		case strings.HasSuffix(base, ".tf"):
			ecosystem = "terraform"
		case strings.Contains(f.path, ".github/workflows/"):
			ecosystem = "githubAction"
		case base == "deno.json" || base == "deno.jsonc":
			ecosystem = "deno"
		case base == "mix.exs" || base == "mix.lock":
			ecosystem = "hex"
		}
		if f.err != nil {
			if ecosystem != "" {
				e.providerError(ecosystem, f.path)
				if ecosystem == "terraform" {
					e.providerError("terraform.resource", f.path)
				}
			}
			continue
		}
		text := string(f.data)
		switch ecosystem {
		case "npm", "composer", "deno":
			var doc map[string]json.RawMessage
			data := f.data
			if base == "deno.jsonc" {
				data = jsonComments(data)
			}
			if json.Unmarshal(data, &doc) != nil {
				e.providerError(ecosystem, f.path)
				break
			}
			for _, key := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies", "require", "require-dev", "imports"} {
				var entries map[string]string
				if json.Unmarshal(doc[key], &entries) != nil {
					continue
				}
				for name, value := range entries {
					if ecosystem == "deno" {
						e.add(ecosystem, value, f.path)
					} else {
						e.add(ecosystem, name, f.path)
					}
				}
			}
		case "python", "cargo":
			if strings.HasSuffix(base, ".toml") || base == "Pipfile" {
				var doc map[string]any
				if toml.Unmarshal(f.data, &doc) != nil {
					e.providerError(ecosystem, f.path)
					break
				}
				e.tomlDependencies(doc, ecosystem, f.path)
			} else {
				for _, line := range strings.Split(text, "\n") {
					name := strings.TrimSpace(line)
					if name == "" || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "-") {
						continue
					}
					parts := strings.FieldsFunc(name, func(r rune) bool { return strings.ContainsRune("[<>=!~; ", r) })
					if len(parts) == 0 {
						e.providerError(ecosystem, f.path)
						continue
					}
					name = parts[0]
					e.add(ecosystem, name, f.path)
				}
			}
		case "hex":
			for _, m := range hexDependency.FindAllStringSubmatch(text, -1) {
				e.add("hex", m[1], f.path)
			}
		case "go", "ruby":
			for _, m := range manifestLine.FindAllStringSubmatch(text, -1) {
				name := m[1]
				if ecosystem == "go" && (!strings.Contains(name, ".") || !strings.Contains(name, "/")) {
					continue
				}
				e.add(ecosystem, name, f.path)
			}
		case "docker":
			if !strings.HasPrefix(base, "Dockerfile") {
				e.compose(f.data, f.path)
				break
			}
			for _, line := range strings.Split(text, "\n") {
				fields := strings.Fields(line)
				if len(fields) > 1 && strings.EqualFold(fields[0], "FROM") {
					index := 1
					if strings.HasPrefix(fields[index], "--") {
						index++
					}
					if index < len(fields) {
						name := fields[index]
						if strings.Contains(name, "$") {
							e.providerError(ecosystem, f.path)
							continue
						}
						name = imageName(name)
						e.add(ecosystem, name, f.path)
					}
				}
				if strings.HasPrefix(strings.TrimSpace(line), "image:") {
					name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:")), "\"'")
					if strings.Contains(name, "$") {
						e.providerError(ecosystem, f.path)
						continue
					}
					e.add(ecosystem, imageName(name), f.path)
				}
			}
		case "terraform":
			e.terraform(f.data, f.path)
		case "githubAction":
			e.actions(f.data, f.path)
		}
		if e.settings.Detection.MCP && (base == ".mcp.json" || strings.HasSuffix(f.path, "/mcp.json") || f.path == ".codex/config.toml") {
			if len(f.data) > 65536 {
				e.providerError("mcp", f.path)
				continue
			}
			var doc map[string]any
			var err error
			if strings.HasSuffix(base, ".toml") {
				err = toml.Unmarshal(f.data, &doc)
			} else {
				err = json.Unmarshal(f.data, &doc)
			}
			if err != nil {
				e.providerError("mcp", f.path)
				continue
			}
			e.mcp(doc, f.path)
		}
	}
}

// manifestMap is the recursive object representation returned by go-toml/yaml JSON data readers.
type manifestMap map[string]any

// tomlDependencies follows github.com/pelletier/go-toml/v2.Unmarshal's recursive document boundary.
func (e *engine) tomlDependencies(doc manifestMap, eco, path string) {
	for key, value := range doc {
		nested, ok := value.(map[string]any)
		if ok {
			if key == "dependencies" || key == "dev-dependencies" || key == "build-dependencies" || key == "packages" || key == "dev-packages" {
				for name, spec := range nested {
					if eco == "cargo" {
						if table, ok := spec.(map[string]any); ok {
							if pkg, ok := table["package"].(string); ok {
								name = pkg
							}
						}
					}
					e.add(eco, name, path)
				}
			}
			if eco == "python" && (key == "optional-dependencies" || key == "dependency-groups") {
				for _, group := range nested {
					if list, ok := group.([]any); ok {
						e.pythonList(list, path)
					}
				}
			}
			e.tomlDependencies(manifestMap(nested), eco, path)
			continue
		}
		list, ok := value.([]any)
		if !ok {
			continue
		}
		if key == "dependencies" || key == "optional-dependencies" {
			e.pythonList(list, path)
		}
	}
}

func (e *engine) providerError(ecosystem, path string) {
	e.providerErrors[ecosystem+"\x00"+e.projectFor(path)] = true
}
