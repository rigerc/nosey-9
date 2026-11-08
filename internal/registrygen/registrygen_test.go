package registrygen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	goTech     = `{"schemaVersion":2,"technologies":[{"id":"node","name":"Node","category":"language","metadataOnly":true},{"id":"go","name":"Go","category":"language","description":"Go language","rules":[{"id":"b","kind":"file","glob":"**/b"},{"id":"a","kind":"file","glob":"**/a"}]}]}`
	goSkills   = `{"schemaVersion":2,"skills":[{"id":"go:skill","name":"Go Skill","source":{"kind":"git","uri":"owner/repo","skillName":"go"}}]}`
	goMappings = `{"schemaVersion":2,"mappings":[{"id":"go:map","technologyId":"go","recommendations":[{"skillId":"go:skill"}]}]}`
)

func writeFile(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixture(t *testing.T, config string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, ConfigName, config)
	for name, data := range files {
		writeFile(t, root, name, data)
	}
	return root
}

func simpleConfig(upstreams string) string {
	extra := ""
	if upstreams != "" {
		extra = `,"upstreams":` + upstreams
	}
	return `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test Registry"},"manifest":"nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"}` + extra + `}`
}

func simpleFixture(t *testing.T) string {
	return fixture(t, simpleConfig(""), map[string]string{
		"technologies.json": goTech,
		"skills.json":       goSkills,
		"mappings.json":     goMappings,
	})
}

func TestGenerateIsDeterministicAndCanonical(t *testing.T) {
	root := simpleFixture(t)
	first, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(first.ChangedFiles, []string{"mappings.json", "nosey.registry.json", "nosey.registry.lock.json", "skills.json", "technologies.json"}) {
		t.Fatalf("unexpected first write: %v", first.ChangedFiles)
	}
	techs := readFile(t, root, "technologies.json")
	if bytes.Index(techs, []byte(`"category"`)) > bytes.Index(techs, []byte(`"description"`)) {
		t.Fatal("field order was reordered:", string(techs))
	}
	if bytes.Index(techs, []byte(`"id": "go"`)) > bytes.Index(techs, []byte(`"id": "node"`)) {
		t.Fatal("entities were not sorted by id:", string(techs))
	}
	if bytes.Index(techs, []byte(`"id": "b"`)) > bytes.Index(techs, []byte(`"id": "a"`)) {
		t.Fatal("nested rule order was rewritten:", string(techs))
	}
	if !bytes.HasSuffix(techs, []byte("}\n")) {
		t.Fatal("output has no trailing newline")
	}
	second, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ChangedFiles) != 0 {
		t.Fatal("second generation was not a no-op:", second.ChangedFiles)
	}
	before := readFile(t, root, "technologies.json")
	if _, err = Run(root, Options{Check: true}); err != nil {
		t.Fatal("check failed after generation:", err)
	}
	if !bytes.Equal(before, readFile(t, root, "technologies.json")) {
		t.Fatal("check wrote to disk")
	}
}

func TestCheckReportsDriftAndGenerateRepairsIt(t *testing.T) {
	root := simpleFixture(t)
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	original := readFile(t, root, "skills.json")
	writeFile(t, root, "skills.json", string(original)+"\n\n")
	_, err := Run(root, Options{Check: true})
	var drift *DriftError
	if !errors.As(err, &drift) || !slicesEqual(drift.Paths, []string{"skills.json"}) {
		t.Fatalf("expected skills drift, got %v", err)
	}
	if !bytes.Equal(readFile(t, root, "skills.json"), append(original, '\n', '\n')) {
		t.Fatal("check modified the drifted file")
	}
	result, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(result.ChangedFiles, []string{"skills.json"}) {
		t.Fatal("repair did not report the changed file:", result.ChangedFiles)
	}
	if _, err = Run(root, Options{Check: true}); err != nil {
		t.Fatal("drift survived regeneration:", err)
	}
}

func TestCheckReportsManifestAndLockDrift(t *testing.T) {
	root := simpleFixture(t)
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "nosey.registry.json", "{\n  \"schemaVersion\": 1\n}\n")
	_, err := Run(root, Options{Check: true})
	var drift *DriftError
	if !errors.As(err, &drift) || !slicesEqual(drift.Paths, []string{"nosey.registry.json"}) {
		t.Fatalf("expected manifest drift, got %v", err)
	}
	if _, err = Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, LockName)); err != nil {
		t.Fatal(err)
	}
	_, err = Run(root, Options{Check: true})
	if !errors.As(err, &drift) || !slicesEqual(drift.Paths, []string{LockName}) {
		t.Fatalf("expected lock drift, got %v", err)
	}
}

func TestInvalidReferenceLeavesFilesUntouched(t *testing.T) {
	root := fixture(t, simpleConfig(""), map[string]string{
		"technologies.json": `{"schemaVersion":2,"technologies":[]}`,
		"skills.json":       goSkills,
		"mappings.json":     goMappings,
	})
	before := readFile(t, root, "mappings.json")
	_, err := Run(root, Options{})
	var input *InputError
	if !errors.As(err, &input) {
		t.Fatalf("expected input error, got %v", err)
	}
	if !bytes.Equal(before, readFile(t, root, "mappings.json")) {
		t.Fatal("invalid generation rewrote an input")
	}
	if _, err = os.Stat(filepath.Join(root, "nosey.registry.json")); !os.IsNotExist(err) {
		t.Fatal("invalid generation wrote a manifest")
	}
}

func TestConfigRejectsUnsafePathsAndCollisions(t *testing.T) {
	for name, config := range map[string]string{
		"escape":      `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"nosey.registry.json","files":{"technologies":"../technologies.json","skills":"skills.json","mappings":"mappings.json"}}`,
		"absolute":    `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"/tmp/nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"}}`,
		"collision":   `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"technologies.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"}}`,
		"duplicate":   `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"nosey.registry.json","files":{"technologies":"same.json","skills":"same.json","mappings":"mappings.json"}}`,
		"missing":     `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"nosey.registry.json","files":{"skills":"skills.json","mappings":"mappings.json"}}`,
		"badupstream": `{"schemaVersion":1,"registry":{"id":"test:registry","name":"Test"},"manifest":"nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"},"upstreams":{"mappings":{"url":"file:///tmp/repo","path":"mappings.json"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t, config, nil)
			if _, err := Run(root, Options{}); err == nil {
				t.Fatal("unsafe config was accepted")
			}
		})
	}
}

type fakeFetcher struct {
	revision string
	data     []byte
	err      error
	calls    int
}

func (f *fakeFetcher) Fetch(context.Context, Upstream) (string, []byte, error) {
	f.calls++
	if f.err != nil {
		return "", nil, f.err
	}
	return f.revision, f.data, nil
}

func TestRefreshReplacesUpstreamAndRelocks(t *testing.T) {
	rev := strings.Repeat("a", 40)
	root := fixture(t, simpleConfig(`{"mappings":{"url":"owner/repo","ref":"main","path":"data/mappings.json"}}`), map[string]string{
		"technologies.json": goTech,
		"skills.json":       goSkills,
		"mappings.json":     goMappings,
	})
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	updated := `{"schemaVersion":2,"mappings":[{"id":"go:map","technologyId":"go","recommendations":[{"skillId":"go:skill","priority":"recommended"}]}]}`
	fetcher := &fakeFetcher{revision: rev, data: []byte(updated)}
	result, err := Refresh(context.Background(), root, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 {
		t.Fatal("upstream was not fetched once")
	}
	if !slicesEqual(result.ChangedFiles, []string{"mappings.json", "nosey.registry.lock.json"}) {
		t.Fatal("unexpected refresh changes:", result.ChangedFiles)
	}
	lock, exists, err := readLock(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if lock.Files["mappings.json"].Revision != rev {
		t.Fatal("resolved revision was not locked:", lock.Files["mappings.json"])
	}
	if !bytes.Contains(readFile(t, root, "mappings.json"), []byte(`"priority": "recommended"`)) {
		t.Fatal("upstream content was not installed")
	}
}

func TestRefreshFailureChangesNothing(t *testing.T) {
	root := fixture(t, simpleConfig(`{"mappings":{"url":"owner/repo","ref":"main","path":"data/mappings.json"}}`), map[string]string{
		"technologies.json": goTech,
		"skills.json":       goSkills,
		"mappings.json":     goMappings,
	})
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, root, "mappings.json")
	beforeLock := readFile(t, root, LockName)
	_, err := Refresh(context.Background(), root, &fakeFetcher{err: errors.New("offline")})
	var refreshErr *RefreshError
	if !errors.As(err, &refreshErr) {
		t.Fatalf("expected refresh error, got %v", err)
	}
	if !bytes.Equal(before, readFile(t, root, "mappings.json")) || !bytes.Equal(beforeLock, readFile(t, root, LockName)) {
		t.Fatal("failed refresh modified files")
	}
}

func TestRefreshWithoutUpstreamsIsANoOp(t *testing.T) {
	root := simpleFixture(t)
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{err: errors.New("must not be called")}
	result, err := Refresh(context.Background(), root, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 0 || len(result.ChangedFiles) != 0 {
		t.Fatal("unexpected refresh work", result.ChangedFiles, fetcher.calls)
	}
}

func TestManifestKeepsTopLevelOrder(t *testing.T) {
	root := simpleFixture(t)
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, root, "nosey.registry.json")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	order := []string{`"schemaVersion"`, `"id"`, `"name"`, `"files"`}
	last := -1
	for _, key := range order {
		index := bytes.Index(data, []byte(key))
		if index <= last {
			t.Fatalf("manifest field %s is out of order:\n%s", key, data)
		}
		last = index
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCanonicalizePreservesUnknownFields(t *testing.T) {
	doc, err := canonicalize("technologies", []byte(`{"schemaVersion":2,"technologies":[{"id":"x","custom":{"keep":true}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doc.canonical, []byte(`"custom": {`)) || !bytes.Contains(doc.canonical, []byte(`"keep": true`)) {
		t.Fatal("unknown field was dropped:", string(doc.canonical))
	}
}
