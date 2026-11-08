package detection

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

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
