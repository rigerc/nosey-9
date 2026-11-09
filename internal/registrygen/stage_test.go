package registrygen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rigerc/nosey-9/internal/storage"
)

const stageConfig = `{"schemaVersion":1,"registry":{"id":"x:y","name":"X"},"manifest":"nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"}}`

const stageSkill = `{"id":"o:r:a","name":"a","technologies":["t"],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"main","skillName":"a"}}`

func stageFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		ConfigName:          stageConfig,
		"technologies.json": `{"schemaVersion":2,"technologies":[{"id":"t","name":"T","category":"tool","rules":[]}]}`,
		"skills.json":       `{"schemaVersion":2,"skills":[` + stageSkill + `]}`,
		"mappings.json":     `{"schemaVersion":2,"mappings":[]}`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStageMatchesRunBytesWithoutWriting(t *testing.T) {
	root := stageFixture(t)
	before, err := storage.Read(root, "skills.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	updated := []byte(`{"schemaVersion":2,"skills":[` + stageSkill[:len(stageSkill)-1] + `,"description":"new"}]}`)
	staged, err := Stage(root, map[string][]byte{"skills": updated})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", staged.Diagnostics)
	}
	after, err := storage.Read(root, "skills.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Stage wrote to disk")
	}
	if _, err = Run(root, Options{Overrides: map[string][]byte{"skills": updated}}); err != nil {
		t.Fatal(err)
	}
	for name, data := range staged.Files {
		written, readErr := storage.Read(root, name, 1<<20)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(written, data) {
			t.Fatalf("staged %s differs from Run output", name)
		}
	}
}

func TestStageReportsEveryProblemByEntity(t *testing.T) {
	root := stageFixture(t)
	skills := []byte(`{"schemaVersion":2,"skills":[` +
		`{"id":"o:r:a","name":"a","disabled":true,"technologies":[],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"main","skillName":"a"}},` +
		`{"id":"o:r:b","name":"b","technologies":["missing"],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"main","skillName":"b"}},` +
		`{"id":"o:r:c","name":"c","technologies":["gone"],"requires":["nope"],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"main","skillName":"c"}}]}`)
	staged, err := Stage(root, map[string][]byte{"skills": skills})
	if err != nil {
		t.Fatal(err)
	}
	subjects := map[string]string{}
	for _, diagnostic := range staged.Diagnostics {
		subjects[diagnostic.Subject] = diagnostic.Code
	}
	if subjects["o:r:a"] != diagnosticSchema || subjects["o:r:b"] != diagnosticReference || subjects["o:r:c"] != diagnosticReference {
		t.Fatalf("diagnostics = %+v", staged.Diagnostics)
	}
	if len(staged.Diagnostics) < 4 {
		t.Fatalf("expected every problem, got %+v", staged.Diagnostics)
	}
}

func TestStageRejectsUnparseableDocument(t *testing.T) {
	root := stageFixture(t)
	if _, err := Stage(root, map[string][]byte{"skills": []byte(`[]`)}); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestValidateReferencesStillReportsFirstProblem(t *testing.T) {
	root := stageFixture(t)
	skills := []byte(`{"schemaVersion":2,"skills":[{"id":"o:r:b","name":"b","technologies":["missing"],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"main","skillName":"b"}}]}`)
	_, err := Run(root, Options{Overrides: map[string][]byte{"skills": skills}})
	if err == nil || err.Error() != "skill o:r:b references unknown technology missing" {
		t.Fatalf("err = %v", err)
	}
}
