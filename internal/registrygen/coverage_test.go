package registrygen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestErrorCodesAndMessages(t *testing.T) {
	drift := &DriftError{Paths: []string{"a.json", "b.json"}}
	if !strings.Contains(drift.Error(), "a.json, b.json") || (&DriftError{}).Error() == "" {
		t.Fatalf("unexpected drift message %q", drift.Error())
	}
	base := errors.New("boom")
	input := &InputError{Err: base}
	refresh := &RefreshError{Err: base}
	if !errors.Is(input, base) || !errors.Is(refresh, base) || input.Error() != "boom" || refresh.Error() != "boom" {
		t.Fatal("wrapped errors must expose the cause")
	}
	for err, want := range map[error]int{nil: 0, drift: 2, input: 2, refresh: 3, base: 3, fmt.Errorf("wrap: %w", drift): 2} {
		if got := Code(err); got != want {
			t.Fatalf("Code(%v) = %d, want %d", err, got, want)
		}
	}
}

func TestFullRevision(t *testing.T) {
	sha := strings.Repeat("a1", 20)
	for value, want := range map[string]bool{sha: true, "main": false, strings.ToUpper(sha): false, sha[:39]: false, strings.Repeat("g", 40): false} {
		if fullRevision(value) != want {
			t.Fatalf("fullRevision(%q) != %v", value, want)
		}
	}
}

func TestReferenceValidation(t *testing.T) {
	skills := func(extra string) string {
		return `{"schemaVersion":2,"skills":[{"id":"go:skill","name":"Go Skill","source":{"kind":"git","uri":"owner/repo","skillName":"go"}` + extra + `},{"id":"go:other","name":"Other","source":{"kind":"git","uri":"owner/repo","skillName":"other"}}]}`
	}
	mappings := func(recommendations string) string {
		return `{"schemaVersion":2,"mappings":[{"id":"go:map","technologyId":"go","recommendations":` + recommendations + `}]}`
	}
	cases := map[string]struct{ skills, mappings string }{
		"unknown repository":    {skills(`,"repositoryId":"missing"`), mappings(`[{"skillId":"go:skill"}]`)},
		"unknown technology":    {skills(`,"technologies":["nope"]`), mappings(`[{"skillId":"go:skill"}]`)},
		"unknown requires":      {skills(`,"requires":["nope"]`), mappings(`[{"skillId":"go:skill"}]`)},
		"requires self":         {skills(`,"requires":["go:skill"]`), mappings(`[{"skillId":"go:skill"}]`)},
		"requires and conflict": {skills(`,"requires":["go:other"],"conflictsWith":["go:other"]`), mappings(`[{"skillId":"go:skill"}]`)},
		"unknown conflict":      {skills(`,"conflictsWith":["nope"]`), mappings(`[{"skillId":"go:skill"}]`)},
		"duplicate skill":       {skills(``), mappings(`[{"skillId":"go:skill"},{"skillId":"go:skill"}]`)},
		"unknown skill":         {skills(``), mappings(`[{"skillId":"nope"}]`)},
		"unknown rec requires":  {skills(``), mappings(`[{"skillId":"go:skill","requires":["nope"]}]`)},
		"unknown when":          {skills(``), mappings(`[{"skillId":"go:skill","when":{"allTechnologies":["nope"]}}]`)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := fixture(t, simpleConfig(""), map[string]string{"technologies.json": goTech, "skills.json": c.skills, "mappings.json": c.mappings})
			_, err := Run(root, Options{})
			var input *InputError
			if !errors.As(err, &input) {
				t.Fatalf("expected input error, got %v", err)
			}
		})
	}
}

const templateConfig = `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"},"templates":[{"id":"empty","kind":"registry","descriptor":"templates/registry/empty/nosey.template.json"}]}`

func templateFixture(t *testing.T, descriptor string) string {
	return fixture(t, templateConfig, map[string]string{
		"technologies.json": goTech,
		"skills.json":       goSkills,
		"mappings.json":     goMappings,
		"templates/registry/empty/nosey.template.json": descriptor,
		"templates/registry/empty/files/a.json":        "{}",
		"templates/registry/empty/files/ci.yml":        "name: ci",
	})
}

func descriptor(generator, files, ci string) string {
	return `{"schemaVersion":1,"id":"empty","kind":"registry","generator":"` + generator + `","description":"d","files":` + files + `,"ci":` + ci + `}`
}

func TestTemplateValidation(t *testing.T) {
	good := `[{"source":"files/a.json","destination":"a.json"}]`
	ci := `[{"provider":"github","source":"files/ci.yml","destination":".github/workflows/r.yml"}]`
	cases := map[string]string{
		"generator":        descriptor("other", good, `[]`),
		"unsafe":           descriptor("registry-v1", `[{"source":"files/a.json","destination":"../a.json"}]`, `[]`),
		"duplicate":        descriptor("registry-v1", `[{"source":"files/a.json","destination":"a.json"},{"source":"files/a.json","destination":"a.json"}]`, `[]`),
		"missing source":   descriptor("registry-v1", `[{"source":"files/none.json","destination":"a.json"}]`, `[]`),
		"ci provider":      descriptor("registry-v1", good, `[{"provider":"gitlab","source":"files/ci.yml","destination":"ci.yml"}]`),
		"ci collides":      descriptor("registry-v1", good, `[{"provider":"github","source":"files/ci.yml","destination":"a.json"}]`),
		"invalid document": `{"schemaVersion":1}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Run(templateFixture(t, doc), Options{})
			var input *InputError
			if !errors.As(err, &input) {
				t.Fatalf("expected input error, got %v", err)
			}
		})
	}
	if _, err := Run(templateFixture(t, descriptor("registry-v1", good, ci)), Options{}); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	root := templateFixture(t, descriptor("registry-v1", good, ci))
	if err := os.Remove(filepath.Join(root, "templates/registry/empty/files/a.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(root, Options{}); err == nil {
		t.Fatal("missing template file accepted")
	}
}

func TestDuplicateTemplateIDRejected(t *testing.T) {
	config := strings.Replace(templateConfig, `"templates":[`, `"templates":[{"id":"empty","kind":"registry","descriptor":"templates/registry/empty/nosey.template.json"},`, 1)
	root := templateFixture(t, descriptor("registry-v1", `[{"source":"files/a.json","destination":"a.json"}]`, `[]`))
	writeFile(t, root, ConfigName, config)
	if _, err := Run(root, Options{}); err == nil {
		t.Fatal("duplicate template id accepted")
	}
}

func TestMovedManifestIsRemoved(t *testing.T) {
	root := simpleFixture(t)
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ConfigName, strings.Replace(simpleConfig(""), `"manifest":"nosey.registry.json"`, `"manifest":"registry.json"`, 1))
	result, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "nosey.registry.json")); !os.IsNotExist(err) {
		t.Fatal("previous manifest was not removed")
	}
	if !slices.Contains(result.RemovedFiles, "nosey.registry.json") {
		t.Fatalf("removed files = %v", result.RemovedFiles)
	}
}
