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

func TestDisplayModeSettingsPreserveLegacyFilesUntilExplicitSave(t *testing.T) {
	s := testStore(t)
	legacy := []byte(`{"schemaVersion":1,"installation":{"agents":["codex"]}}`)
	if err := storage.Write(s.UserPath, legacy); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil || r.Settings.UI.Mode != "fullscreen" || r.Settings.Version != 3 {
		t.Fatal("legacy settings did not receive defaults", r, err)
	}
	before, err := os.ReadFile(s.UserPath)
	if err != nil || string(before) != string(legacy) {
		t.Fatal("reading legacy settings rewrote them", err)
	}
	if err = s.Set("ui.mode", []byte(`"invalid"`), false); err == nil {
		t.Fatal("invalid display mode accepted")
	}
	if err = s.Set("ui.mode", []byte(`true`), false); err == nil {
		t.Fatal("wrong display mode type accepted")
	}
	if err = s.Set("ui.mode", []byte(`"inline"`), false); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(s.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Version int `json:"schemaVersion"`
	}
	if json.Unmarshal(saved, &header) != nil || header.Version != 3 {
		t.Fatal("new preference did not use settings v3")
	}
	r, err = s.Resolve()
	if err != nil || r.Settings.UI.Mode != "inline" || len(r.Settings.Installation.Agents) != 1 {
		t.Fatal("display save lost other settings", r, err)
	}
	if err = s.Set("updates.ttlHours", []byte(`12`), false); err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil || r.Settings.UI.Mode != "inline" {
		t.Fatal("later save lost display preference", err)
	}
}

func writeUser(t *testing.T, s Store, doc string) {
	t.Helper()
	if err := storage.Write(s.UserPath, []byte(doc)); err != nil {
		t.Fatal(err)
	}
}

// savedSettings is the part of a written settings file the migration tests inspect.
type savedSettings struct {
	Version      int                        `json:"schemaVersion"`
	Installation map[string]json.RawMessage `json:"installation"`
}

func savedVersion(t *testing.T, path string) savedSettings {
	t.Helper()
	// #nosec G304 -- test helper reading the settings file in its temporary store.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc savedSettings
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestV1AndV2MigrateToV3OnLoadAndPersistOnSet(t *testing.T) {
	retired := `"installation":{"agents":["codex"],"defaultMode":"print","cliVersion":"1.7.6","upstreamTelemetry":"disabled","method":"copy"}`
	for version, doc := range map[int]string{1: `{"schemaVersion":1,` + retired + `}`, 2: `{"schemaVersion":2,"ui":{"mode":"inline"},` + retired + `}`} {
		s := testStore(t)
		writeUser(t, s, doc)
		r, err := s.Resolve()
		if err != nil || r.Settings.Version != 3 || r.Settings.Installation.Method != "copy" || len(r.Settings.Installation.Agents) != 1 || len(r.Settings.Installation.CustomAgents) != 0 {
			t.Fatal(version, r.Settings, err)
		}
		if before, _ := os.ReadFile(s.UserPath); string(before) != doc {
			t.Fatal("load rewrote the file", version)
		}
		if err = s.Set("updates.ttlHours", []byte(`12`), false); err != nil {
			t.Fatal(version, err)
		}
		saved := savedVersion(t, s.UserPath)
		for _, key := range []string{"defaultMode", "cliVersion", "upstreamTelemetry"} {
			if _, ok := saved.Installation[key]; ok {
				t.Fatalf("v%d: retired key %s survived the write", version, key)
			}
		}
		if saved.Version != 3 || string(saved.Installation["method"]) != `"copy"` {
			t.Fatal(version, saved)
		}
	}
}

func TestRetiredKeysAreRejectedByV3Writes(t *testing.T) {
	s := testStore(t)
	for _, key := range []string{"installation.defaultMode", "installation.cliVersion", "installation.upstreamTelemetry"} {
		if err := s.Set(key, []byte(`"print"`), false); err == nil {
			t.Fatal("retired key accepted", key)
		}
	}
	if err := s.Set("installation.verbose", []byte(`true`), false); err != nil {
		t.Fatal(err)
	}
	if v := savedVersion(t, s.UserPath).Version; v != 3 {
		t.Fatal("new file is not v3", v)
	}
}

func TestCustomAgentsValidation(t *testing.T) {
	good := `[{"id":"my-agent","name":"My Agent","skillsDir":".my-agent/skills"}]`
	s := testStore(t)
	if err := s.Set("installation.customAgents", []byte(good), false); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil || len(r.Settings.Installation.CustomAgents) != 1 || r.Settings.Installation.CustomAgents[0].SkillsDir != ".my-agent/skills" {
		t.Fatal(r.Settings.Installation.CustomAgents, err)
	}
	for name, doc := range map[string]string{
		"absolute dir":  `[{"id":"a","name":"A","skillsDir":"/etc"}]`,
		"escaping dir":  `[{"id":"a","name":"A","skillsDir":"../x"}]`,
		"dot dir":       `[{"id":"a","name":"A","skillsDir":"."}]`,
		"backslash dir": `[{"id":"a","name":"A","skillsDir":"x\\y"}]`,
		"unsafe id":     `[{"id":"../a","name":"A","skillsDir":"x"}]`,
		"uppercase id":  `[{"id":"A","name":"A","skillsDir":"x"}]`,
		"empty name":    `[{"id":"a","name":"","skillsDir":"x"}]`,
		"missing dir":   `[{"id":"a","name":"A"}]`,
		"extra field":   `[{"id":"a","name":"A","skillsDir":"x","global":true}]`,
		"duplicate id":  `[{"id":"a","name":"A","skillsDir":"x"},{"id":"a","name":"B","skillsDir":"y"}]`,
		"builtin id":    `[{"id":"codex","name":"A","skillsDir":"x"}]`,
		"control name":  `[{"id":"a","name":"A\u001b[31m","skillsDir":"x"}]`,
		"not an array":  `{"id":"a"}`,
	} {
		if err = testStore(t).Set("installation.customAgents", []byte(doc), false); err == nil {
			t.Fatal("accepted:", name)
		}
	}
}

func TestProjectSettingsRejectCustomAgents(t *testing.T) {
	s := testStore(t)
	agents := `[{"id":"my-agent","name":"My Agent","skillsDir":".my-agent/skills"}]`
	if err := s.Set("installation.customAgents", []byte(agents), true); err == nil {
		t.Fatal("project write accepted customAgents")
	}
	if err := s.Set("installation", []byte(`{"customAgents":`+agents+`}`), true); err == nil {
		t.Fatal("project write of installation accepted customAgents")
	}
	if err := storage.Write(filepath.Join(s.Workspace, ".nosey", "config.json"), []byte(`{"schemaVersion":3,"installation":{"customAgents":`+agents+`}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(); err == nil {
		t.Fatal("project file with customAgents resolved")
	}
	if err := storage.Write(filepath.Join(s.Workspace, ".nosey", "config.json"), []byte(`{"schemaVersion":3,"installation":{"method":"copy"}}`)); err != nil {
		t.Fatal(err)
	}
	if r, err := s.Resolve(); err != nil || r.Settings.Installation.Method != "copy" {
		t.Fatal(r.Settings.Installation, err)
	}
}
