package catalogimport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const testCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func fixture(t *testing.T) Inputs {
	t.Helper()
	return Inputs{
		Revision:      testCommit,
		Ledger:        []byte(`{"schema_version":1,"entries":[{"id":"owner/repo/folder","url":"https://github.com/owner/renamed/blob/` + testCommit + `/skills/folder/SKILL.md","commit":"` + testCommit + `","source_skill_name":"actual-name"},{"id":"owner/repo/bonus","url":"https://github.com/owner/renamed/blob/` + testCommit + `/skills/bonus/SKILL.md","commit":"` + testCommit + `"}]}`),
		Catalog:       []byte(`{"schema_version":1,"skills":[{"id":"owner/repo/folder","skill_name":"folder"},{"id":"owner/repo/bonus","skill_name":"bonus"}]}`),
		Mappings:      []byte(`{"schema_version":1,"technologies":[{"id":"node","name":"Node","category":"language","skill_ids":["owner/repo/folder"]},{"id":"new-tech","name":"New technology","category":"New Things","skill_ids":[]}],"combinations":[{"id":"stack","name":"Stack","requires":["node","new-tech"],"skill_ids":["owner/repo/folder"]}],"frontend_essentials":{"skill_ids":["owner/repo/bonus"]}}`),
		Technologies:  []byte(`{"schemaVersion":2,"technologies":[{"id":"nodejs","name":"NodeJS","category":"language","rules":[{"id":"file","kind":"file","glob":"**/package.json"}]}]}`),
		TechnologyMap: []byte(`{"node":"nodejs","new-tech":"new-tech"}`),
	}
}

func TestCompilePreservesSourcesAndConditionalMappings(t *testing.T) {
	in := fixture(t)
	r, err := Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Skills []skill `json:"skills"`
	}
	decode(t, r.Files["skills.json"], &doc)
	if len(doc.Skills) != 2 {
		t.Fatal(doc)
	}
	for _, s := range doc.Skills {
		if s.ID == "owner:repo:folder" && (s.Source.URI != "https://github.com/owner/renamed" || s.Source.Ref != testCommit || s.Source.SkillName != "actual-name") {
			t.Fatalf("source metadata lost: %+v", s)
		}
		if s.ID == "owner:repo:bonus" && len(s.Technologies) != 0 {
			t.Fatal("frontend-only skill got technology associations")
		}
	}
	var md struct {
		Mappings []mapping `json:"mappings"`
	}
	decode(t, r.Files["mappings.json"], &md)
	if len(md.Mappings) != 2 {
		t.Fatal(md)
	}
	for _, m := range md.Mappings {
		if len(m.Recommendations) != 1 || m.Recommendations[0].SkillID != "owner:repo:folder" {
			t.Fatal(m)
		}
		if m.ID == "combination:stack" {
			if m.TechnologyID != "new-tech" || m.Recommendations[0].When == nil || !slices.Equal(m.Recommendations[0].When.All, []string{"nodejs"}) {
				t.Fatalf("stack AND condition lost: %+v", m)
			}
		} else if m.Recommendations[0].When != nil {
			t.Fatal("unconditional mapping got stack condition")
		}
	}
	var td technologyDocument
	decode(t, r.Files["technologies.json"], &td)
	if len(td.Technologies) != 2 {
		t.Fatal(td)
	}
	var added struct {
		MetadataOnly bool `json:"metadataOnly"`
	}
	decode(t, td.Technologies[0], &added)
	if !added.MetadataOnly {
		t.Fatal("new identity must be metadata-only")
	}
	if string(r.Files["authoring/technology-reference.json"]) != string(in.Technologies) {
		t.Fatal("reference bytes changed")
	}
}

func TestCompileRejectsBrokenReferencesAndSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Inputs)
	}{
		{"missing crosswalk", func(in *Inputs) { in.TechnologyMap = []byte(`{"node":"nodejs"}`) }},
		{"unknown skill", func(in *Inputs) {
			in.Mappings = []byte(strings.ReplaceAll(string(in.Mappings), "owner/repo/folder", "unknown/repo/skill"))
		}},
		{"unknown stack technology", func(in *Inputs) {
			in.Mappings = []byte(strings.Replace(string(in.Mappings), `"requires":["node","new-tech"]`, `"requires":["missing"]`, 1))
		}},
		{"duplicate ledger ID", func(in *Inputs) {
			var l ledgerDocument
			decode(t, in.Ledger, &l)
			l.Entries = append(l.Entries, l.Entries[0])
			in.Ledger = encode(t, l)
		}},
		{"wrong commit", func(in *Inputs) {
			in.Ledger = []byte(strings.Replace(string(in.Ledger), "/blob/"+testCommit+"/", "/blob/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/", 1))
		}},
		{"path traversal", func(in *Inputs) {
			in.Ledger = []byte(strings.Replace(string(in.Ledger), "/skills/folder/", "/skills/../folder/", 1))
		}},
		{"option-like name", func(in *Inputs) { in.Ledger = []byte(strings.Replace(string(in.Ledger), "actual-name", "--all", 1)) }},
		{"unsupported version", func(in *Inputs) {
			in.Catalog = []byte(strings.Replace(string(in.Catalog), `"schema_version":1`, `"schema_version":2`, 1))
		}},
		{"ambiguous install name", func(in *Inputs) { in.Ledger = []byte(strings.Replace(string(in.Ledger), "actual-name", "bonus", 1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := fixture(t)
			tc.change(&in)
			if _, err := Compile(in); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestCompileDeterministicAcrossInputOrder(t *testing.T) {
	in := fixture(t)
	a, err := Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	var l ledgerDocument
	decode(t, in.Ledger, &l)
	slices.Reverse(l.Entries)
	in.Ledger = encode(t, l)
	var m mappingDocument
	decode(t, in.Mappings, &m)
	slices.Reverse(m.Technologies)
	in.Mappings = encode(t, m)
	b, err := Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	// The manifest hashes input bytes, which intentionally change after reordering.
	delete(a.Files, "authoring/import-manifest.json")
	delete(b.Files, "authoring/import-manifest.json")
	if !reflect.DeepEqual(a.Files, b.Files) {
		t.Fatal("runtime generation depends on input ordering")
	}
}

func TestProvidedCatalog(t *testing.T) {
	root := filepath.Join("..", "..")
	inputRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inputRoot.Close() })
	read := func(path string) []byte {
		t.Helper()
		data, err := inputRoot.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	in := Inputs{
		Revision: "d1bc68a826cd233f85f291eca0cfa9b4303713e6",
		Ledger:   read("registry-import/inputs/catalog-source-ledger.json"), Catalog: read("registry-import/inputs/catalog.json"), Mappings: read("registry-import/inputs/mappings.json"),
		TechnologyMap: read("registry-import/technology-id-map.json"), Technologies: read("docs/prd/source-planning-package/reference/technology-registry-v2/technologies-894-v2.json"),
	}
	r, err := Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Skills != 877 || r.Technologies != 898 || r.Combinations != 35 || r.Mappings != 258 {
		t.Fatalf("unexpected counts: %+v", r)
	}
	// All original definitions (including rules and scoped IDs) remain unchanged.
	var original, generated technologyDocument
	decode(t, in.Technologies, &original)
	decode(t, r.Files["technologies.json"], &generated)
	index, err := technologyIndex(generated.Technologies)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range original.Technologies {
		var entity struct {
			ID string `json:"id"`
		}
		decode(t, raw, &entity)
		var a, b any
		decode(t, raw, &a)
		decode(t, index[entity.ID], &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("original definition changed: %s", entity.ID)
		}
	}
	second, err := Compile(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Files, second.Files) {
		t.Fatal("repeated import differs")
	}
}

func decode[T any](t *testing.T, data []byte, target *T) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func encode[T any](t *testing.T, value T) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
