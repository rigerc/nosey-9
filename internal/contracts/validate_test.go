package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAllEmbeddedContractsCompileAndRejectWrongVersion(t *testing.T) {
	entries, err := files.ReadDir("schemas")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			if err := Validate(entry.Name(), []byte(`{"schemaVersion":999,"outputVersion":999}`)); err == nil {
				t.Fatal("invalid document passed")
			}
			if _, ok := cache.Load(entry.Name()); !ok {
				t.Fatal("schema failed to compile")
			}
		})
	}
}
func TestOfficialDocuments(t *testing.T) {
	for _, item := range []struct{ file, schema string }{{"nosey.registry.json", "registry.schema.json"}, {"skills.json", "skills.effective.schema.json"}, {"mappings.json", "mappings.effective.schema.json"}, {"repositories.json", "repositories.schema.json"}, {"technologies.json", "technologies.v2.schema.json"}} {
		// #nosec G304 -- path selects an application-owned file or a checked-in contract fixture.
		data, err := os.ReadFile(filepath.Join("..", "..", "registries", "default", item.file))
		if os.IsNotExist(err) {
			t.Skip("official registry integration submodule not initialized")
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = Validate(item.schema, data); err != nil {
			t.Fatalf("%s: %v", item.file, err)
		}
	}
}
func TestSchemaCopiesMatchApprovedSource(t *testing.T) {
	entries, err := files.ReadDir("schemas")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		source := filepath.Join("..", "..", "docs", "prd", "source-planning-package", "schemas", e.Name())
		// #nosec G304 -- path selects an application-owned file or a checked-in contract fixture.
		data, err := os.ReadFile(source)
		if os.IsNotExist(err) {
			// #nosec G304 -- path selects an application-owned file or a checked-in contract fixture.
			data, err = os.ReadFile(filepath.Join("..", "..", "docs", "prd", "schemas", e.Name()))
		}
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := files.ReadFile("schemas/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if json.Unmarshal(data, &a) != nil || json.Unmarshal(embedded, &b) != nil {
			t.Fatal("invalid JSON")
		}
		if string(data) != string(embedded) {
			t.Fatalf("contract copy drift: %s", e.Name())
		}
	}
}
