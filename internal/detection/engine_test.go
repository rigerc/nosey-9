package detection

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

func TestWorkspaceEvidenceMCPAndSecretRedaction(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"package.json": `{"dependencies":{"next":"1"}}`, ".env": "SECRET_TOKEN=do-not-leak-value\n", ".mcp.json": `{"mcpServers":{"context":{"command":"npx","args":["-y","@upstash/context7-mcp@1"]}}}`}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := domain.Catalog{Technologies: map[string]domain.Technology{"next": {ID: "next", Rules: []domain.Rule{{ID: "dep", Kind: "dependency", Ecosystem: "npm", Name: "next"}}}, "env": {ID: "env", Rules: []domain.Rule{{ID: "key", Kind: "dotenv", Key: "SECRET_TOKEN"}}}, "mcp": {ID: "mcp", Rules: []domain.Rule{{ID: "pkg", Kind: "mcp_package", Name: "@upstash/context7-mcp"}}}, "metadata": {ID: "metadata", MetadataOnly: true}}}
	cfg := settings.Defaults()
	r, err := Scan(context.Background(), root, c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.States["next"] != "detected" || r.States["env"] != "detected" || r.States["mcp"] != "unknown" || r.Coverage.Unevaluated != 0 || r.Coverage.Excluded != 1 {
		t.Fatal(r)
	}
	for _, d := range r.Detections {
		for _, e := range d.Evidence {
			if strings.Contains(e.Identifier, "do-not-leak") {
				t.Fatal("dotenv value leaked")
			}
		}
	}
	cfg.Detection.MCP = true
	r, err = Scan(context.Background(), root, c, cfg)
	if err != nil || r.States["mcp"] != "detected" {
		t.Fatal(r, err)
	}
}
func TestOversizeRequiredContentIsUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("large secret value"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Detection.MaxBytes = 2
	c := domain.Catalog{Technologies: map[string]domain.Technology{"test": {ID: "test", Rules: []domain.Rule{{ID: "read", Kind: "content", Glob: "**/*.txt", Contains: "large"}}}}}
	r, err := Scan(context.Background(), root, c, cfg)
	if err != nil || r.States["test"] != "unknown" || r.Coverage.Unevaluated != 1 {
		t.Fatal(r, err)
	}
}
func TestExactRootDoesNotAscend(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "package.json"), []byte(`{"dependencies":{"next":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "child")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	c := domain.Catalog{Technologies: map[string]domain.Technology{"next": {ID: "next", Rules: []domain.Rule{{ID: "dep", Kind: "dependency", Ecosystem: "npm", Name: "next"}}}}}
	r, err := Scan(context.Background(), root, c, settings.Defaults())
	if err != nil || r.States["next"] != "absent" {
		t.Fatal(r, err)
	}
}

func TestNestedProjectScopeAndDirectoryEvidence(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{"package.json": `{"dependencies":{}}`, "a/package.json": `{"dependencies":{"next":"1"}}`, "a/src/file.js": "export {}"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := domain.Catalog{Technologies: map[string]domain.Technology{"next": {ID: "next", Rules: []domain.Rule{{ID: "dep", Kind: "dependency", Ecosystem: "npm", Name: "next"}, {ID: "folder", Kind: "file", Glob: "**/src"}}}}}
	r, err := Scan(context.Background(), root, c, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.Required != 4 || len(r.Detections) != 1 {
		t.Fatal(r)
	}
	for _, e := range r.Detections[0].Evidence {
		if e.ProjectPath != "a" {
			t.Fatalf("evidence assigned to directory instead of project: %+v", e)
		}
	}
}

func TestPositiveEvidenceWinsOverAnotherProjectProviderFailure(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{"package.json": `{"dependencies":{"next":"1"}}`, "broken/package.json": `{broken`} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := domain.Catalog{Technologies: map[string]domain.Technology{"next": {ID: "next", Rules: []domain.Rule{{ID: "dep", Kind: "dependency", Ecosystem: "npm", Name: "next"}}}}}
	r, err := Scan(context.Background(), root, c, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if r.States["next"] != "detected" || r.Coverage.Required != 2 || r.Coverage.Evaluated != 1 || r.Coverage.Unevaluated != 1 {
		t.Fatalf("positive match or scoped coverage lost: %+v", r)
	}
}
func TestMCPArgumentsDoNotLeakSecretsOrRunUnrelatedCommands(t *testing.T) {
	root := t.TempDir()
	data := `{"mcpServers":{"good":{"command":"npx","args":["-y","@upstash/context7-mcp@1","--api-key","do-not-leak"]},"unrelated":{"command":"echo","args":["@other/server"]},"off":{"command":"uvx","args":["disabled-server"],"enabled":false}}}`
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Detection.MCP = true
	c := domain.Catalog{Technologies: map[string]domain.Technology{"mcp": {ID: "mcp", Rules: []domain.Rule{{ID: "package", Kind: "mcp_package", NameRegex: ".*"}}}}}
	r, err := Scan(context.Background(), root, c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Detections) != 1 || len(r.Detections[0].Evidence) != 1 || r.Detections[0].Evidence[0].Identifier != "@upstash/context7-mcp" {
		t.Fatalf("MCP interpreted server arguments or arbitrary commands as packages: %+v", r)
	}
}
func TestTerraformAndActionCommentsAreNotDependencies(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"main.tf": "# resource \"aws_instance\" \"fake\" {}\nlocals {\n text = <<EOF\nresource \"aws_instance\" \"text\" {}\nEOF\n}\nresource \"azurerm_resource_group\" \"real\" {}\nterraform {\n required_providers {\n azurerm = { source = \"hashicorp/azurerm\" }\n }\n}\n", ".github/workflows/test.yml": "# uses: actions/fake@v1\njobs:\n  test:\n    steps:\n      - uses: actions/checkout@v4\n"}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := domain.Catalog{Technologies: map[string]domain.Technology{"aws": {ID: "aws", Rules: []domain.Rule{{ID: "resource", Kind: "dependency", Ecosystem: "terraform.resource", Name: "aws_instance"}}}, "azure": {ID: "azure", Rules: []domain.Rule{{ID: "provider", Kind: "dependency", Ecosystem: "terraform", Name: "registry.terraform.io/hashicorp/azurerm"}}}, "checkout": {ID: "checkout", Rules: []domain.Rule{{ID: "action", Kind: "dependency", Ecosystem: "githubAction", Name: "actions/checkout"}}}, "fake": {ID: "fake", Rules: []domain.Rule{{ID: "comment", Kind: "dependency", Ecosystem: "githubAction", Name: "actions/fake"}}}}}
	r, err := Scan(context.Background(), root, c, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if r.States["aws"] != "absent" || r.States["fake"] != "absent" || r.States["azure"] != "detected" || r.States["checkout"] != "detected" {
		t.Fatal(r)
	}
}

func TestManifestProvidersAndOptionalDependencies(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"package.json":   `{"dependencies":{"next":"1"}}`,
		"pyproject.toml": "[project]\ndependencies = [\"Django>=5\"]\n[project.optional-dependencies]\nqueue = [\"celery[redis]>=5\"]\n",
		"Cargo.toml":     "[dependencies]\nrenamed = { package = \"tokio\", version = \"1\" }\n",
		"go.mod":         "module example.org/test\nrequire github.com/jackc/pgx/v5 v5.0.0\n",
		"Gemfile":        "gem 'rails', '~> 7'\n",
		"composer.json":  `{"require":{"laravel/framework":"*"}}`,
		"deno.jsonc":     "{ // comment\n \"imports\": {\"db\":\"https://deno.land/x/postgres@v1/mod.ts\",},\n}",
		"compose.yml":    "services:\n  db:\n    image: registry.local:5000/postgres:17\n",
		"Dockerfile":     "FROM --platform=linux/amd64 redis:7 AS cache\n",
		"mix.exs":        "defp deps do\n [{:phoenix, \"~> 1.7\"}]\nend\n",
	}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	targets := map[string]string{"npm": "next", "python": "celery", "cargo": "tokio", "go": "github.com/jackc/pgx/v5", "ruby": "rails", "composer": "laravel/framework", "deno": "https://deno.land/x/postgres@v1/mod.ts", "docker": "registry.local:5000/postgres", "hex": "phoenix"}
	c := domain.Catalog{Technologies: map[string]domain.Technology{}}
	for eco, name := range targets {
		c.Technologies[eco] = domain.Technology{ID: eco, Rules: []domain.Rule{{ID: "dep", Kind: "dependency", Ecosystem: eco, Name: name}}}
	}
	r, err := Scan(context.Background(), root, c, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	for eco := range targets {
		if r.States[eco] != "detected" {
			t.Fatalf("%s provider missed dependency: %+v", eco, r)
		}
	}
	if r.Coverage.Unevaluated != 0 {
		t.Fatal(r.Coverage)
	}
}

func TestTrustedAdaptersRemainScopedAndFailuresProduceGaps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "proof.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := filepath.Join(t.TempDir(), "adapter")
	cfg := settings.Defaults()
	c := domain.Catalog{Technologies: map[string]domain.Technology{"custom": {ID: "custom", Rules: []domain.Rule{{ID: "rule", Kind: "detector", Detector: "test"}}}}}
	for _, tc := range []struct{ body, want string }{{`[{"path":"proof.txt","projectPath":"forged","identifier":"secret"}]`, "detected"}, {`[{"path":"../outside"}]`, "unknown"}, {`{invalid`, "unknown"}} {
		script := []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + tc.body + "'\n")
		// #nosec G306 -- trusted adapter fixture must be executable.
		if err := os.WriteFile(adapter, script, 0o700); err != nil {
			t.Fatal(err)
		}
		cfg.Detection.Adapters = []settings.Adapter{{ID: "test", Version: "1", Hash: storage.Hash(script), Path: adapter, Enabled: true}}
		result, err := Scan(context.Background(), root, c, cfg)
		if err != nil || result.States["custom"] != tc.want {
			t.Fatal(result, err)
		}
		if tc.want == "detected" {
			ev := result.Detections[0].Evidence[0]
			if ev.Identifier != "" || ev.ProjectPath != "." {
				t.Fatal("adapter controlled provenance or exposed secrets", ev)
			}
		}
	}
	cfg.Detection.Adapters[0].Hash = strings.Repeat("0", 64)
	result, err := Scan(context.Background(), root, c, cfg)
	if err != nil || result.Coverage.Unevaluated != 1 {
		t.Fatal("adapter signature mismatch was treated as absence", result, err)
	}
}
