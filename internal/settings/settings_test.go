package settings

import (
	"encoding/json"
	"github.com/rigerc/nosey-9/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestDefaultsAndTypedSettings(t *testing.T) {
	s := testStore(t)
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.Detection.MCP || r.Settings.Updates.TTL != 24 {
		t.Fatal("incorrect defaults")
	}
	if err = s.Set("updates.ttlHours", []byte(`0`), false); err == nil {
		t.Fatal("invalid TTL accepted")
	}
	if err = s.Set("installation.agents", []byte(`["codex"]`), false); err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil || len(r.Settings.Installation.Agents) != 1 {
		t.Fatal(r, err)
	}
}
func TestWorkspaceTrustChangesAndScope(t *testing.T) {
	s := testStore(t)
	path := filepath.Join(s.Workspace, ".nosey", "config.json")
	data := []byte(`{"schemaVersion":1,"registries":[{"id":"workspace:test","source":{"kind":"local","path":"registry"}}]}`)
	if err := storage.Write(path, data); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Pending) != 1 || len(r.Settings.Registries) != 1 {
		t.Fatal("untrusted registry activated")
	}
	if err = s.Trust(r.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil || len(r.Pending) != 0 || len(r.Settings.Registries) != 2 {
		t.Fatal(r, err)
	}
	if r.Settings.Registries[1].Priority != 30 || r.Settings.Registries[1].Source.Path != filepath.Join(s.Workspace, "registry") {
		t.Fatal("project defaults/path base lost")
	}
	data = []byte(`{"schemaVersion":1,"registries":[{"id":"workspace:test","source":{"kind":"local","path":"changed"}}]}`)
	if err = storage.Write(path, data); err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil || len(r.Pending) != 1 {
		t.Fatal("changed fingerprint retained trust")
	}
	if err = s.Set("detection.adapters", []byte(`[]`), true); err == nil {
		t.Fatal("project adapter config accepted")
	}
}
func TestAtomicArrayReplacementAndPresence(t *testing.T) {
	s := testStore(t)
	if err := s.Set("installation.agents", []byte(`["codex"]`), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("installation.agents", []byte(`[]`), true); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("detection.enableMcpPackage", []byte(`true`), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("detection.enableMcpPackage", []byte(`false`), true); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.Detection.MCP || len(r.Settings.Installation.Agents) != 0 {
		t.Fatal("false/empty override lost")
	}
}
func TestUserPathsAndInvalidFiles(t *testing.T) {
	s := testStore(t)
	if err := storage.Write(s.UserPath, []byte(`{"schemaVersion":1,"registries":[{"id":"relative","source":{"kind":"local","path":"relative"}}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(); err == nil {
		t.Fatal("relative user path accepted")
	}
	if err := os.WriteFile(s.UserPath, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	_ = json.Valid(nil)
}

func TestRegistryByIDMergePreservesOmittedScalars(t *testing.T) {
	s := testStore(t)
	data := []byte(`{"schemaVersion":1,"registries":[{"id":"same","source":{"kind":"local","path":"/tmp/base"},"enabled":false,"priority":0,"ttlHours":12}]}`)
	if err := storage.Write(s.UserPath, data); err != nil {
		t.Fatal(err)
	}
	if err := storage.Write(filepath.Join(s.Workspace, ".nosey/config.json"), []byte(`{"schemaVersion":1,"registries":[{"id":"same","source":{"kind":"local","path":"override"}}]}`)); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Trust(r.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	ref := r.Settings.Registries[1]
	if ref.Enabled || ref.Priority != 0 || ref.TTLHours != 12 {
		t.Fatalf("omitted scalar erased inherited value: %+v", ref)
	}
}

func TestProjectSettingsRejectEscapingSymlinks(t *testing.T) {
	s := testStore(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Workspace, ".nosey")); err != nil {
		t.Skip(err)
	}
	if err := s.Set("installation.method", []byte(`"copy"`), true); err == nil {
		t.Fatal("project settings write escaped workspace")
	}
	if _, err := os.Stat(filepath.Join(outside, "config.json")); !os.IsNotExist(err) {
		t.Fatal("outside file created", err)
	}
}

func TestRegistryEditsStayUserScopedAndTrustCanBeRevoked(t *testing.T) {
	s := testStore(t)
	if refs, err := s.UserRegistries(); err != nil || len(refs) != 0 {
		t.Fatal(refs, err)
	}
	if err := s.Set("registries", []byte(`[{"id":"user:test","source":{"kind":"local","path":"/tmp/registry"}}]`), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("registries", []byte(`[{"id":"workspace:test","source":{"kind":"local","path":"registry"}}]`), true); err != nil {
		t.Fatal(err)
	}
	refs, err := s.UserRegistries()
	if err != nil || len(refs) != 1 || refs[0].ID != "user:test" {
		t.Fatal("project proposals leaked into user edits", refs, err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Trust("wrong", false); err == nil {
		t.Fatal("wrong workspace fingerprint approved")
	}
	if err = s.Trust(r.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Trust("", true); err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil || len(r.Pending) != 1 {
		t.Fatal("revoked proposal remained enabled", r, err)
	}
	for _, key := range []string{"schemaVersion", "unknown", "a.b.c"} {
		if err = s.Set(key, []byte(`1`), false); err == nil {
			t.Fatal("invalid setting key accepted", key)
		}
	}
}
