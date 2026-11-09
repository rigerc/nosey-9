package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

func readScaffoldFile(t *testing.T, root string) scaffold {
	t.Helper()
	record, err := readScaffold(root)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestCheckCIFileConfinesDestinations(t *testing.T) {
	for _, destination := range []string{".git/hooks/pre-commit", "../outside", ".github/../.git/config", "workflow.yml", `.github\x.yml`} {
		if checkCIFile("github", destination) == nil {
			t.Errorf("accepted %s", destination)
		}
	}
	if checkCIFile("gitlab", ".github/x.yml") == nil {
		t.Error("accepted unknown provider")
	}
	if err := checkCIFile("github", ".github/workflows/x.yml"); err != nil {
		t.Error(err)
	}
}

const (
	workflowOne = "name: one\nenv:\n  GENERATOR_REF: 1111111111111111111111111111111111111111\n"
	workflowTwo = "name: two\nenv:\n  GENERATOR_REF: 2222222222222222222222222222222222222222\n"
	workflowDst = ".github/workflows/registry.yml"
)

type ciFixture struct {
	t              *testing.T
	provider, root string
	catalog        domain.Catalog
}

// newCIFixture creates a template provider and a registry generated from it.
func newCIFixture(t *testing.T, workflow string) *ciFixture {
	t.Helper()
	f := &ciFixture{t: t, provider: t.TempDir()}
	f.publish([]map[string]string{{"provider": "github", "source": "files/registry.yml", "destination": workflowDst}}, map[string]string{"files/registry.yml": workflow})
	f.catalog = domain.Catalog{Roots: map[string]string{"source:one": f.provider}}
	f.root = t.TempDir()
	record := scaffold{Version: scaffoldVersion, Template: "empty", Source: "source:one", CreatedBy: "test"}
	record.recordCI("github", workflowDst, []byte(workflow))
	f.write(workflowDst, workflow)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	f.write(scaffoldName, string(encoded))
	return f
}

func (f *ciFixture) write(name, content string) {
	f.t.Helper()
	if err := storage.Write(filepath.Join(f.root, name), []byte(content)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *ciFixture) read(name string) string {
	f.t.Helper()
	data, err := storage.Read(f.root, name, 1<<20)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *ciFixture) publish(ci []map[string]string, files map[string]string) {
	f.t.Helper()
	descriptor, err := json.Marshal(map[string]any{"schemaVersion": 1, "id": "empty", "kind": "registry", "generator": "registry-v1", "description": "t", "files": []map[string]string{{"source": "files/registry.yml", "destination": "keep.yml"}}, "ci": ci})
	if err != nil {
		f.t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"id":"source:one","name":"S","files":{"technologies":"t.json","skills":"s.json","mappings":"m.json"},"templates":[{"id":"empty","kind":"registry","descriptor":"tpl/nosey.template.json"}]}`
	all := map[string]string{"nosey.registry.json": manifest, "tpl/nosey.template.json": string(descriptor)}
	for name, content := range files {
		all["tpl/"+name] = content
	}
	for name, content := range all {
		if err = storage.Write(filepath.Join(f.provider, name), []byte(content)); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *ciFixture) update(force bool) (CIUpdateResult, error) {
	f.t.Helper()
	return Service{}.UpdateCI(f.catalog, CIUpdateOptions{Path: f.root, Force: force})
}

func TestUpdateCIReplacesUneditedFile(t *testing.T) {
	f := newCIFixture(t, workflowOne)
	f.publish([]map[string]string{{"provider": "github", "source": "files/registry.yml", "destination": workflowDst}}, map[string]string{"files/registry.yml": workflowTwo})
	result, err := f.update(false)
	if err != nil || !reflect.DeepEqual(result.Updated, []string{workflowDst}) {
		t.Fatal(result, err)
	}
	if f.read(workflowDst) != workflowTwo {
		t.Fatal(f.read(workflowDst))
	}
	if !strings.HasPrefix(result.GeneratorFrom, "1111") || !strings.HasPrefix(result.GeneratorTo, "2222") {
		t.Fatal(result)
	}
	record := readScaffoldFile(t, f.root)
	if record.CIFiles[workflowDst] != storage.Hash([]byte(workflowTwo)) || !strings.HasPrefix(record.GeneratorRef, "2222") {
		t.Fatal(record)
	}
	again, err := f.update(false)
	if err != nil || len(again.Updated) != 0 || !reflect.DeepEqual(again.Unchanged, []string{workflowDst}) {
		t.Fatal(again, err)
	}
}

func TestUpdateCIRefusesLocalEditsUntilForced(t *testing.T) {
	f := newCIFixture(t, workflowOne)
	f.publish([]map[string]string{{"provider": "github", "source": "files/registry.yml", "destination": workflowDst}}, map[string]string{"files/registry.yml": workflowTwo})
	f.write(workflowDst, "edited")
	if _, err := f.update(false); err == nil || !strings.Contains(err.Error(), workflowDst) {
		t.Fatal(err)
	}
	if f.read(workflowDst) != "edited" {
		t.Fatal("conflict must not write")
	}
	if _, err := f.update(true); err != nil || f.read(workflowDst) != workflowTwo {
		t.Fatal(err)
	}
}

func TestUpdateCIAddsNewFilesAndRemovesRetiredOnes(t *testing.T) {
	f := newCIFixture(t, workflowOne)
	f.publish([]map[string]string{{"provider": "github", "source": "files/dependabot.yml", "destination": ".github/dependabot.yml"}}, map[string]string{"files/dependabot.yml": "version: 2\n", "files/registry.yml": workflowOne})
	result, err := f.update(false)
	if err != nil || !reflect.DeepEqual(result.Added, []string{".github/dependabot.yml"}) || !reflect.DeepEqual(result.Removed, []string{workflowDst}) {
		t.Fatal(result, err)
	}
	if _, err = os.Stat(filepath.Join(f.root, workflowDst)); !os.IsNotExist(err) {
		t.Fatal("retired file left behind")
	}
}

func TestUpdateCIKeepsEditedRetiredFile(t *testing.T) {
	f := newCIFixture(t, workflowOne)
	f.publish([]map[string]string{{"provider": "github", "source": "files/dependabot.yml", "destination": ".github/dependabot.yml"}}, map[string]string{"files/dependabot.yml": "version: 2\n", "files/registry.yml": workflowOne})
	f.write(workflowDst, "edited")
	result, err := f.update(false)
	if err != nil || !reflect.DeepEqual(result.Kept, []string{workflowDst}) || f.read(workflowDst) != "edited" {
		t.Fatal(result, err)
	}
}

func TestUpdateCIDeletedFileIsAConflict(t *testing.T) {
	f := newCIFixture(t, workflowOne)
	if err := os.Remove(filepath.Join(f.root, workflowDst)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.update(false); err == nil {
		t.Fatal("a deliberately deleted file must not silently return")
	}
	if result, err := f.update(true); err != nil || len(result.Added) != 1 {
		t.Fatal(result, err)
	}
}

func TestUpdateCIFromOlderScaffoldNeedsForceOnlyWhenDifferent(t *testing.T) {
	f := newCIFixture(t, workflowOne)
	f.write(scaffoldName, `{"schemaVersion":1,"templateId":"empty","sourceRegistryId":"source:one","sourceRevision":null,"createdBy":"old","ci":["github"]}`)
	result, err := f.update(false)
	if err != nil || len(result.Unchanged) != 1 {
		t.Fatal(result, err)
	}
	if record := readScaffoldFile(t, f.root); record.Version != scaffoldVersion || record.CIFiles[workflowDst] == "" {
		t.Fatal(record)
	}
	f.write(scaffoldName, `{"schemaVersion":1,"templateId":"empty","sourceRegistryId":"source:one","sourceRevision":null,"createdBy":"old","ci":["github"]}`)
	f.write(workflowDst, "older generated copy")
	if _, err = f.update(false); err == nil {
		t.Fatal("unrecorded differing file replaced without --force")
	}
}

func TestUpdateCIErrors(t *testing.T) {
	if _, err := (Service{}).UpdateCI(domain.Catalog{}, CIUpdateOptions{Path: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "registry init") {
		t.Fatal(err)
	}
	f := newCIFixture(t, workflowOne)
	if _, err := (Service{}).UpdateCI(domain.Catalog{}, CIUpdateOptions{Path: f.root}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatal(err)
	}
	if _, err := (Service{}).UpdateCI(f.catalog, CIUpdateOptions{Path: f.root, RegistryID: "other:one"}); err == nil {
		t.Fatal("unknown provider override accepted")
	}
	f.publish([]map[string]string{{"provider": "github", "source": "files/registry.yml", "destination": ".github/x.yml"}}, map[string]string{"files/registry.yml": workflowOne})
	f.write(scaffoldName, `{"schemaVersion":3}`)
	if _, err := f.update(false); err == nil {
		t.Fatal("invalid scaffold accepted")
	}
}

// The real template, end to end: a new scaffold is current, valid and idempotent.
func TestScaffoldRoundTripFromPublishedTemplate(t *testing.T) {
	result, err := initFixture(t, InitOptions{CI: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registrygen.Run(result.Path, registrygen.Options{Check: true}); err != nil {
		t.Fatal("new scaffold is out of date:", err)
	}
	record := readScaffoldFile(t, result.Path)
	if record.Version != scaffoldVersion || !reflect.DeepEqual(record.CI, []string{"github"}) || record.CIFiles[workflowDst] == "" || record.GeneratorRef == "" {
		t.Fatalf("%+v", record)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := domain.Catalog{Roots: map[string]string{"nosey:official": root}}
	updated, err := (Service{}).UpdateCI(catalog, CIUpdateOptions{Path: result.Path})
	if err != nil || len(updated.Updated)+len(updated.Added)+len(updated.Removed) != 0 {
		t.Fatal(updated, err)
	}
}
