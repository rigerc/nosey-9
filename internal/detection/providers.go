package detection

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"gopkg.in/yaml.v3"
)

func (e *engine) terraform(data []byte, path string) {
	parsed, diagnostics := hclsyntax.ParseConfig(data, path, hcl.Pos{Line: 1, Column: 1})
	if diagnostics.HasErrors() {
		e.providerError("terraform", path)
		e.providerError("terraform.resource", path)
		return
	}
	body, ok := parsed.Body.(*hclsyntax.Body)
	if !ok {
		e.providerError("terraform", path)
		return
	}
	for _, block := range body.Blocks {
		if block.Type == "resource" && len(block.Labels) > 0 {
			e.add("terraform.resource", block.Labels[0], path)
		}
		if block.Type != "terraform" {
			continue
		}
		for _, required := range block.Body.Blocks {
			if required.Type != "required_providers" {
				continue
			}
			for local, attribute := range required.Body.Attributes {
				source := "registry.terraform.io/hashicorp/" + local
				pairs, diags := hcl.ExprMap(attribute.Expr)
				if diags.HasErrors() {
					value, evalDiags := attribute.Expr.Value(nil)
					if evalDiags.HasErrors() || value.Type() != cty.String {
						e.providerError("terraform", path)
						continue
					}
					e.add("terraform", source, path)
					continue
				}
				valid := true
				for _, pair := range pairs {
					key, keyDiags := pair.Key.Value(nil)
					if keyDiags.HasErrors() || key.Type() != cty.String {
						valid = false
						break
					}
					if key.AsString() != "source" {
						continue
					}
					value, valueDiags := pair.Value.Value(nil)
					if valueDiags.HasErrors() || !value.IsKnown() || value.Type() != cty.String {
						valid = false
						break
					}
					source = value.AsString()
					if strings.Count(source, "/") == 1 {
						source = "registry.terraform.io/" + source
					}
				}
				if !valid {
					e.providerError("terraform", path)
					continue
				}
				e.add("terraform", source, path)
			}
		}
	}
}
func yamlField(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
func (e *engine) actions(data []byte, path string) {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil {
		e.providerError("githubAction", path)
		return
	}
	add := func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind != yaml.ScalarNode || strings.Contains(n.Value, "${{") {
			e.providerError("githubAction", path)
			return
		}
		e.add("githubAction", strings.Split(n.Value, "@")[0], path)
	}
	steps := func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind != yaml.SequenceNode {
			e.providerError("githubAction", path)
			return
		}
		for _, step := range n.Content {
			if yamlField(step, "<<") != nil || step.Kind == yaml.AliasNode {
				e.providerError("githubAction", path)
			}
			add(yamlField(step, "uses"))
		}
	}
	jobs := yamlField(&doc, "jobs")
	if jobs != nil {
		if jobs.Kind != yaml.MappingNode {
			e.providerError("githubAction", path)
			return
		}
		for i := 1; i < len(jobs.Content); i += 2 {
			job := jobs.Content[i]
			if yamlField(job, "<<") != nil || job.Kind == yaml.AliasNode {
				e.providerError("githubAction", path)
			}
			add(yamlField(job, "uses"))
			steps(yamlField(job, "steps"))
		}
	}
	steps(yamlField(yamlField(&doc, "runs"), "steps"))
}

type mcpServer struct {
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	Disabled bool     `json:"disabled"`
	Enabled  *bool    `json:"enabled"`
}
type mcpDocument struct {
	Camel   map[string]mcpServer `json:"mcpServers"`
	Snake   map[string]mcpServer `json:"mcp_servers"`
	Servers map[string]mcpServer `json:"servers"`
}

func (e *engine) mcp(doc manifestMap, path string) {
	data, err := json.Marshal(doc)
	if err != nil {
		e.providerError("mcp", path)
		return
	}
	var parsed mcpDocument
	if json.Unmarshal(data, &parsed) != nil {
		e.providerError("mcp", path)
		return
	}
	for _, servers := range []map[string]mcpServer{parsed.Camel, parsed.Snake, parsed.Servers} {
		for _, server := range servers {
			if server.Disabled || server.Enabled != nil && !*server.Enabled {
				continue
			}
			command := filepath.Base(server.Command)
			args := server.Args
			switch command {
			case "npx", "bunx", "uvx":
			case "npm":
				if len(args) == 0 || args[0] != "exec" {
					continue
				}
				args = args[1:]
			case "pnpm", "yarn":
				if len(args) == 0 || args[0] != "dlx" {
					continue
				}
				args = args[1:]
			case "uv":
				if len(args) < 2 || args[0] != "tool" || args[1] != "run" {
					continue
				}
				args = args[2:]
			default:
				continue
			}
			packages := []string{}
			flagPackage := false
			for i := 0; i < len(args); i++ {
				arg := args[i]
				if arg == "-p" || arg == "--package" || arg == "--from" {
					if i+1 >= len(args) {
						e.providerError("mcp", path)
						break
					}
					i++
					packages = append(packages, args[i])
					flagPackage = true
					continue
				}
				if strings.HasPrefix(arg, "--package=") || strings.HasPrefix(arg, "--from=") {
					_, value, _ := strings.Cut(arg, "=")
					packages = append(packages, value)
					flagPackage = true
					continue
				}
				if strings.HasPrefix(arg, "-") {
					if !strings.Contains(arg, "=") && !slicesContains([]string{"-y", "--yes", "--no-install", "--quiet", "--", "--no-progress"}, arg) {
						e.providerError("mcp", path)
						break
					}
					continue
				}
				if !flagPackage {
					packages = append(packages, arg)
				}
				break
			}
			for _, pkg := range packages {
				if strings.ContainsAny(pkg, "$=") || strings.Contains(pkg, "://") {
					e.providerError("mcp", path)
					continue
				}
				if strings.HasPrefix(pkg, ".") || strings.HasPrefix(pkg, "/") {
					continue
				}
				if strings.HasPrefix(pkg, "@") {
					if i := strings.LastIndex(pkg, "@"); i > 0 {
						pkg = pkg[:i]
					}
				} else {
					pkg = strings.Split(pkg, "@")[0]
				}
				e.add("mcp", pkg, path)
			}
		}
	}
}
func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func imageName(value string) string {
	value = strings.Split(value, "@")[0]
	if i := strings.LastIndex(value, ":"); i > strings.LastIndex(value, "/") {
		value = value[:i]
	}
	return value
}

func (e *engine) pythonList(list []any, path string) {
	for _, item := range list {
		name, ok := item.(string)
		if !ok {
			continue
		}
		parts := strings.FieldsFunc(name, func(r rune) bool { return strings.ContainsRune("[<>=!~; @", r) })
		if len(parts) > 0 {
			e.add("python", strings.ReplaceAll(strings.ToLower(parts[0]), "_", "-"), path)
		}
	}
}
func (e *engine) compose(data []byte, path string) {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil {
		e.providerError("docker", path)
		return
	}
	services := yamlField(&doc, "services")
	if services == nil {
		return
	}
	if services.Kind != yaml.MappingNode {
		e.providerError("docker", path)
		return
	}
	for i := 1; i < len(services.Content); i += 2 {
		service := services.Content[i]
		if service.Kind == yaml.AliasNode || yamlField(service, "<<") != nil {
			e.providerError("docker", path)
		}
		image := yamlField(service, "image")
		if image == nil {
			continue
		}
		if image.Kind != yaml.ScalarNode || strings.Contains(image.Value, "$") {
			e.providerError("docker", path)
			continue
		}
		e.add("docker", imageName(image.Value), path)
	}
}

// jsonComments accepts JSONC comments and trailing commas without changing strings.
func jsonComments(data []byte) []byte {
	out := append([]byte(nil), data...)
	quoted, escaped := false, false
	for i := 0; i < len(out); i++ {
		b := out[i]
		if quoted {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				quoted = false
			}
			continue
		}
		if b == '"' {
			quoted = true
			continue
		}
		if b == '/' && i+1 < len(out) && out[i+1] == '/' {
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		} else if b == '/' && i+1 < len(out) && out[i+1] == '*' {
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i+1 < len(out) && (out[i] != '*' || out[i+1] != '/') {
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i++
			} else {
				return data
			}
		}
	}
	quoted, escaped = false, false
	for i, b := range out {
		if quoted {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				quoted = false
			}
			continue
		}
		if b == '"' {
			quoted = true
			continue
		}
		if b == ',' {
			j := i + 1
			for j < len(out) && strings.ContainsRune(" \n\r\t", rune(out[j])) {
				j++
			}
			if j < len(out) && (out[j] == ']' || out[j] == '}') {
				out[i] = ' '
			}
		}
	}
	return out
}
