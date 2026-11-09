package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	testHost = "studio.test"
	skillA   = "o:r:a"
	skillB   = "o:r:b"
	commitA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func skillJSON(id, extra string) string {
	name := id[strings.LastIndex(id, ":")+1:]
	return `{"id":"` + id + `","name":"` + name + `","technologies":["react"],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"` + commitA + `","skillName":"` + name + `"},"repositoryId":"o:r"` + extra + `}`
}

const assessed = `,"quality":"good","score":80,"assessment":{"method":"automated","assessedAt":"2026-01-01"}`

func writeRegistry(t *testing.T, root, id string) {
	t.Helper()
	files := map[string]string{
		registrygen.ConfigName: `{"schemaVersion":1,"registry":{"id":"` + id + `","name":"Test"},"manifest":"nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json","repositories":"repositories.json"}}`,
		"technologies.json":    `{"schemaVersion":2,"technologies":[{"id":"react","name":"React","category":"ui","rules":[{"id":"file-0001","kind":"file","glob":"**/react.config"}]},{"id":"vue","name":"Vue","category":"ui","rules":[{"id":"file-0001","kind":"file","glob":"**/vue.config"}]}]}`,
		"skills.json":          `{"schemaVersion":2,"skills":[` + skillJSON(skillA, assessed) + `,` + skillJSON(skillB, assessed) + `]}`,
		"mappings.json":        `{"schemaVersion":2,"mappings":[{"id":"technology:react","technologyId":"react","recommendations":[{"skillId":"` + skillA + `","priority":"optional","reason":"why"}]}]}`,
		"repositories.json":    `{"schemaVersion":1,"repositories":[{"id":"o:r","name":"o/r","kind":"git","uri":"https://github.com/o/r"}]}`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registrygen.Run(root, registrygen.Options{}); err != nil {
		t.Fatal(err)
	}
}

// payload is a free-form JSON test request body.
type payload map[string]any

type harness struct {
	t      *testing.T
	server *Server
}

func testStore(t *testing.T) settings.Store {
	t.Helper()
	dir := t.TempDir()
	return settings.Store{Workspace: dir, UserPath: filepath.Join(dir, "user.json"), Cache: filepath.Join(dir, "cache"), Data: filepath.Join(dir, "data")}
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	if opts.Store.Data == "" {
		opts.Store = testStore(t)
	}
	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	server.Allow(testHost)
	return &harness{t, server}
}

func fixture(t *testing.T, id string, checks bool) (*harness, string) {
	t.Helper()
	root := t.TempDir()
	writeRegistry(t, root, id)
	return newHarness(t, Options{Root: root, RegistryChecks: checks}), root
}

func (h *harness) call(method, path string, body payload, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Host = testHost
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func (h *harness) api(path string, body payload) *httptest.ResponseRecorder {
	h.t.Helper()
	method := http.MethodPost
	if body == nil {
		method = http.MethodGet
	}
	return h.call(method, path, body, map[string]string{tokenHeader: h.server.Token()})
}

type change = map[string]any

func (h *harness) stage(ops ...change) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.api("/api/draft", map[string]any{"ops": ops})
}

func raw(text string) json.RawMessage { return json.RawMessage(text) }

func wantStatus(t *testing.T, got *httptest.ResponseRecorder, status int) {
	t.Helper()
	if got.Code != status {
		t.Fatalf("status = %d, want %d: %s", got.Code, status, got.Body.String())
	}
}

func decode[T any](t *testing.T, got *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(got.Body.Bytes(), &value); err != nil {
		t.Fatalf("%v: %s", err, got.Body.String())
	}
	return value
}

func TestGuardRejectsForeignRequests(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wrongHost := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	wrongHost.Host = "evil.test"
	recorder := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(recorder, wrongHost)
	wantStatus(t, recorder, http.StatusForbidden)
	wantStatus(t, h.call(http.MethodGet, "/api/state", nil, map[string]string{"Origin": "http://evil.test", tokenHeader: h.server.Token()}), http.StatusForbidden)
	wantStatus(t, h.call(http.MethodGet, "/api/state", nil, nil), http.StatusUnauthorized)
	wantStatus(t, h.call(http.MethodGet, "/api/state", nil, map[string]string{tokenHeader: "wrong"}), http.StatusUnauthorized)
	wantStatus(t, h.call(http.MethodGet, "/api/state", nil, map[string]string{"Origin": "http://" + testHost, tokenHeader: h.server.Token()}), http.StatusOK)
}

func TestStaticAssetsAreServedWithSafeHeaders(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	page := h.call(http.MethodGet, "/", nil, nil)
	wantStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("missing CSP: %v", page.Header())
	}
	for _, name := range []string{"/app.mjs", "/logic.mjs", "/vendor/htm-preact.mjs"} {
		script := h.call(http.MethodGet, name, nil, nil)
		wantStatus(t, script, http.StatusOK)
		if !strings.Contains(script.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("%s served as %q", name, script.Header().Get("Content-Type"))
		}
	}
}

func TestStateReportsDocumentsAndNoDiagnostics(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	got := h.api("/api/state", nil)
	wantStatus(t, got, http.StatusOK)
	state := decode[State](t, got)
	if len(state.Documents[roleSkills]) != 2 || state.Meta.Automated {
		t.Fatalf("state = %+v", state.Meta)
	}
	if hasErrors(state.Diagnostics) {
		t.Fatalf("diagnostics = %+v", state.Diagnostics)
	}
}

func TestDraftSurvivesRestartAndLeavesTreeClean(t *testing.T) {
	root := t.TempDir()
	writeRegistry(t, root, "x:y")
	store := testStore(t)
	before, err := storage.Read(root, "skills.json", maxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{Root: root, Store: store})
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"description":"x"`))}), http.StatusOK)
	after, err := storage.Read(root, "skills.json", maxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("staging wrote to the registry")
	}
	reopened := newHarness(t, Options{Root: root, Store: store})
	if len(reopened.server.draft.Ops) != 1 || reopened.server.draft.Ops[0].Base == nil {
		t.Fatalf("draft = %+v", reopened.server.draft)
	}
}

func TestDraftDropsOpsThatMatchBase(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"description":"x"`))}), http.StatusOK)
	got := h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, assessed))})
	wantStatus(t, got, http.StatusOK)
	if result := decode[draftResult](t, got); len(result.Ops) != 0 {
		t.Fatalf("ops = %+v", result.Ops)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": "o:r:new", "entity": raw(skillJSON("o:r:new", ""))}), http.StatusOK)
	got = h.stage(change{"role": roleSkills, "id": "o:r:new", "delete": true})
	if result := decode[draftResult](t, got); len(result.Ops) != 0 {
		t.Fatalf("new+delete should cancel: %+v", result.Ops)
	}
}

func TestDraftRejectsMalformedChanges(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	for _, op := range []change{
		{"role": "nope", "id": "a", "entity": raw(`{"id":"a"}`)},
		{"role": roleSkills, "id": "", "entity": raw(`{"id":""}`)},
		{"role": roleSkills, "id": skillA},
		{"role": roleSkills, "id": skillA, "entity": raw(`{"id":"other"}`)},
		{"role": roleSkills, "id": skillA, "entity": raw(`[]`)},
	} {
		wantStatus(t, h.stage(op), http.StatusBadRequest)
	}
}

func TestWriterKeepsProjectedFieldsLastAndArraysPresent(t *testing.T) {
	got, err := normalizeEntity(roleSkills, raw(`{"id":"a","quality":"good","score":1,"assessment":{},"tags":["x"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"a","tags":["x"],"technologies":[],"quality":"good","score":1,"assessment":{}}`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
	technology, err := normalizeEntity(roleTechnologies, raw(`{"id":"t","metadataOnly":true}`))
	if err != nil || !strings.Contains(string(technology), `"rules":[]`) {
		t.Fatalf("technology = %s, %v", technology, err)
	}
	mapping, err := normalizeEntity(roleMappings, raw(`{"id":"m"}`))
	if err != nil || !strings.Contains(string(mapping), `"recommendations":[]`) {
		t.Fatalf("mapping = %s, %v", mapping, err)
	}
}

func TestOwnershipOnAutomatedRegistry(t *testing.T) {
	h, _ := fixture(t, officialID, false)
	withSource := strings.Replace(skillJSON(skillA, assessed), commitA, strings.Repeat("b", 40), 1)
	valid := `{"id":"o:r:n","name":"n","technologies":[],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"` + commitA + `","skillName":"n"},"repositoryId":"o:r"}`
	denied := []change{
		{"role": roleSkills, "id": skillA, "entity": raw(withSource)},
		{"role": roleSkills, "id": skillA, "entity": raw(strings.Replace(skillJSON(skillA, assessed), `"score":80`, `"score":10`, 1))},
		{"role": roleSkills, "id": skillA, "entity": raw(strings.Replace(skillJSON(skillA, assessed), `"repositoryId":"o:r"`, `"repositoryId":"o:other"`, 1))},
		{"role": roleSkills, "id": skillA, "delete": true},
		{"role": roleSkills, "id": "o:r:n", "entity": raw(strings.Replace(valid, commitA, "main", 1))},
		{"role": roleSkills, "id": "o:r:n", "entity": raw(strings.Replace(valid, "https://github.com/o/r", "https://gitlab.com/o/r", 1))},
		{"role": roleSkills, "id": "o:r:n", "entity": raw(strings.Replace(valid, `"repositoryId":"o:r"`, `"repositoryId":"o:r","score":5`, 1))},
	}
	for _, op := range denied {
		wantStatus(t, h.stage(op), http.StatusForbidden)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": "o:r:n", "entity": raw(valid)}), http.StatusForbidden)
	for _, location := range []Location{{Commit: strings.Repeat("b", 40), Path: "n/SKILL.md"}, {Commit: commitA, Path: "n/README.md"}, {Commit: commitA, Path: "../SKILL.md"}} {
		wantStatus(t, h.stage(change{"role": roleSkills, "id": "o:r:n", "entity": raw(valid), "location": location}), http.StatusForbidden)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": "o:r:n", "entity": raw(valid), "location": Location{Commit: commitA, Path: "n/SKILL.md"}}), http.StatusOK)
	edited := strings.Replace(valid, `"name":"n"`, `"name":"renamed"`, 1)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": "o:r:n", "entity": raw(edited)}), http.StatusOK)
	if op := h.server.draft.Ops[0]; op.Location == nil || op.Location.Path != "n/SKILL.md" {
		t.Fatalf("editing a new skill must keep its location: %+v", op)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["curated"]`+assessed))}), http.StatusOK)
}

func TestApplyRecordsNewSkillLocations(t *testing.T) {
	h, root := fixture(t, officialID, false)
	if err := storage.Write(filepath.Join(root, locationsPath), []byte("{\n  \"schema_version\": 1,\n  \"registry_id\": \"nosey:official\",\n  \"locations\": {\n    \"z:z:z\": {\n      \"commit\": \"`+commitA+`\",\n      \"path\": \"z/SKILL.md\"\n    }\n  }\n}\n")); err != nil {
		t.Fatal(err)
	}
	valid := `{"id":"o:r:n","name":"n","technologies":[],"source":{"kind":"git","uri":"https://github.com/o/r","ref":"` + commitA + `","skillName":"n"},"repositoryId":"o:r"}`
	wantStatus(t, h.stage(change{"role": roleSkills, "id": "o:r:n", "entity": raw(valid), "location": Location{Commit: commitA, Path: "n/SKILL.md"}}), http.StatusOK)
	wantStatus(t, h.api("/api/apply", payload{}), http.StatusOK)
	data, err := storage.Read(root, locationsPath, maxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	var document locationsFile
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Locations["o:r:n"].Path != "n/SKILL.md" || document.Locations["z:z:z"].Path != "z/SKILL.md" {
		t.Fatalf("locations = %s", data)
	}
	if strings.Index(string(data), "o:r:n") > strings.Index(string(data), "z:z:z") {
		t.Fatalf("locations must stay sorted: %s", data)
	}
}

func TestScriptFailureKeepsTheEnd(t *testing.T) {
	long := strings.Repeat("x", maxFailureBytes*2) + "the real failure"
	got := scriptFailure(process.Output{Stdout: []byte(long)})
	if !strings.HasSuffix(got, "the real failure") || len(got) > maxFailureBytes+8 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestRegistryChecksUnlockElevatedEdits(t *testing.T) {
	h, _ := fixture(t, officialID, true)
	manual := `,"quality":"poor","score":10,"assessment":{"method":"manual-review","assessedAt":"2026-02-02"}`
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, manual))}), http.StatusOK)
	automated := strings.Replace(manual, "manual-review", "automated", 1)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, automated))}), http.StatusForbidden)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillB, "delete": true}), http.StatusOK)
}

func TestSkillDeleteBlockedByOpenProbe(t *testing.T) {
	h, root := fixture(t, officialID, true)
	if err := storage.Write(filepath.Join(root, "assessments/pin-probes.json"), []byte(`{"schema_version":1,"probes":{"`+skillB+`":{"reason":"repo_gone"}}}`)); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillB, "delete": true}), http.StatusForbidden)
}

func TestNonAutomatedRegistryAllowsAssessmentEdits(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(strings.Replace(skillJSON(skillA, assessed), `"score":80`, `"score":10`, 1))}), http.StatusOK)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillB, "delete": true}), http.StatusOK)
}

func diagnosticCodes(t *testing.T, got *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	codes := map[string]bool{}
	for _, diagnostic := range decode[draftResult](t, got).Diagnostics {
		codes[diagnostic.Code] = true
	}
	return codes
}

func TestDiagnosticsCatchWhatGenerateAlone(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	oneSided := h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"conflictsWith":["`+skillB+`"]`))})
	wantStatus(t, oneSided, http.StatusOK)
	if !diagnosticCodes(t, oneSided)["E_SKILL_REFERENCE"] {
		t.Fatalf("one-sided conflict missed: %s", oneSided.Body.String())
	}
	disabled := h.stage(change{"role": roleSkills, "id": skillB, "entity": raw(skillJSON(skillB, `,"disabled":true`))})
	if !diagnosticCodes(t, disabled)["E_STAGE_SCHEMA"] {
		t.Fatalf("disabled skill missed: %s", disabled.Body.String())
	}
}

func TestTechnologyWithoutRulesMustBeMetadataOnly(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	got := h.stage(change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Vue","category":"ui"}`)})
	wantStatus(t, got, http.StatusOK)
	result := decode[draftResult](t, got)
	found := false
	for _, diagnostic := range result.Diagnostics {
		found = found || (diagnostic.Code == "E_EFFECTIVE_ENTITY" && diagnostic.Subject == "vue")
	}
	if !found {
		t.Fatalf("diagnostics must name the entity by bare id: %+v", result.Diagnostics)
	}
	got = h.stage(change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Vue","category":"ui","metadataOnly":true}`)})
	if hasErrors(decode[draftResult](t, got).Diagnostics) {
		t.Fatalf("metadata-only technology is valid: %s", got.Body.String())
	}
}

func TestRuleDiagnostics(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	got := h.stage(
		change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Vue","category":"ui","rules":[{"id":"a","kind":"file","pathRegex":"("},{"id":"a","kind":"file","glob":"[x"}]}`)},
		change{"role": roleMappings, "id": "technology:vue", "entity": raw(`{"id":"technology:vue","technologyId":"vue","recommendations":[]}`)},
	)
	wantStatus(t, got, http.StatusOK)
	result := decode[draftResult](t, got)
	messages := []string{}
	for _, diagnostic := range result.Diagnostics {
		messages = append(messages, diagnostic.Code+": "+diagnostic.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{"invalid regex", "invalid glob", "Duplicate rule id", "W_STUDIO_EMPTY_MAPPING"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestReplayMergesAutomationChurnAndFlagsRealConflicts(t *testing.T) {
	base := raw(skillJSON(skillA, assessed))
	repinned := raw(strings.Replace(skillJSON(skillA, `,"quality":"poor","score":20,"assessment":{"method":"automated","assessedAt":"2026-03-03"}`), commitA, strings.Repeat("c", 40), 1))
	edit := raw(skillJSON(skillA, `,"tags":["x"]`+assessed))
	newDoc := func(entities ...json.RawMessage) map[string]*document {
		doc := &document{version: raw("2"), byID: map[string]json.RawMessage{}}
		for _, entity := range entities {
			var header struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(entity, &header); err != nil {
				t.Fatal(err)
			}
			doc.put(header.ID, entity)
		}
		return map[string]*document{roleSkills: doc}
	}
	docs := newDoc(repinned)
	conflicts, err := replay([]Op{{Role: roleSkills, ID: skillA, Base: base, Entity: edit}}, docs, true)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("conflicts = %+v, err = %v", conflicts, err)
	}
	merged := string(docs[roleSkills].byID[skillA])
	if !strings.Contains(merged, `"tags":["x"]`) || !strings.Contains(merged, strings.Repeat("c", 40)) || !strings.Contains(merged, `"score":20`) {
		t.Fatalf("merged = %s", merged)
	}
	if strings.Index(merged, `"tags"`) > strings.Index(merged, `"quality"`) {
		t.Fatalf("projected fields must stay last: %s", merged)
	}
	cases := map[string]struct {
		op      Op
		current []json.RawMessage
	}{
		"curated change": {Op{Role: roleSkills, ID: skillA, Base: base, Entity: edit}, []json.RawMessage{raw(skillJSON(skillA, `,"tags":["other"]`+assessed))}},
		"deleted":        {Op{Role: roleSkills, ID: skillA, Base: base, Entity: edit}, nil},
		"created":        {Op{Role: roleSkills, ID: skillA, Entity: edit}, []json.RawMessage{base}},
		"delete changed": {Op{Role: roleSkills, ID: skillA, Base: base}, []json.RawMessage{raw(skillJSON(skillA, `,"tags":["other"]`+assessed))}},
	}
	for name, testCase := range cases {
		found, replayErr := replay([]Op{testCase.op}, newDoc(testCase.current...), true)
		if replayErr != nil || len(found) != 1 {
			t.Fatalf("%s: conflicts = %+v, err = %v", name, found, replayErr)
		}
	}
}

func TestPreviewUsesDraftAndEvaluatesConditions(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wantStatus(t, h.stage(change{"role": roleMappings, "id": "combination:vue-react", "entity": raw(`{"id":"combination:vue-react","technologyId":"vue","recommendations":[{"skillId":"` + skillB + `","priority":"core","when":{"allTechnologies":["react"]}}]}`)}), http.StatusOK)
	got := h.api("/api/preview", map[string]any{"detected": []string{"vue"}})
	wantStatus(t, got, http.StatusOK)
	if result := decode[previewResult](t, got); len(result.Skills) != 0 {
		t.Fatalf("react is absent, want none: %+v", result)
	}
	got = h.api("/api/preview", map[string]any{"detected": []string{"vue", "react"}})
	result := decode[previewResult](t, got)
	priorities := map[string]string{}
	for i := range result.Skills {
		priorities[result.Skills[i].ID] = result.Skills[i].Priority
	}
	if priorities[skillB] != "core" || priorities[skillA] != "optional" {
		t.Fatalf("priorities = %+v", priorities)
	}
	got = h.api("/api/preview", map[string]any{"detected": []string{"vue"}, "unknown": []string{"react"}})
	result = decode[previewResult](t, got)
	unevaluated := map[string]bool{}
	for i := range result.Unevaluated {
		unevaluated[result.Unevaluated[i].SkillID] = true
	}
	if !unevaluated[skillB] || len(result.Skills) != 0 {
		t.Fatalf("unknown condition should be unevaluated: %+v", result)
	}
}

func TestPreviewBlockedByValidationErrors(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(strings.Replace(skillJSON(skillA, ""), `["react"]`, `["missing"]`, 1))}), http.StatusOK)
	got := h.api("/api/preview", map[string]any{"detected": []string{"react"}})
	if result := decode[previewResult](t, got); !result.Blocked {
		t.Fatalf("want blocked: %s", got.Body.String())
	}
}

func TestRuleTesterUsesRealEngine(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	technology := raw(`{"id":"go-tool","name":"G","category":"tool","rules":[{"id":"f","kind":"file","glob":"**/app.conf"},{"id":"d","kind":"detector","detector":"detectX"}]}`)
	got := h.api("/api/rules/test", map[string]any{"technology": technology, "files": []sampleFile{{Path: "nested/app.conf", Content: "x"}}})
	wantStatus(t, got, http.StatusOK)
	result := decode[ruleTestResult](t, got)
	if result.State != stateDetected || len(result.Evidence) != 1 || result.Evidence[0].RuleID != "f" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Gaps) != 1 || result.Gaps[0].RuleID != "d" {
		t.Fatalf("detector rule should be reported as a gap: %+v", result.Gaps)
	}
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "other.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := raw(`{"id":"t","name":"T","category":"tool","rules":[{"id":"f","kind":"file","glob":"**/app.conf"}]}`)
	got = h.api("/api/rules/test", map[string]any{"technology": plain, "folder": folder})
	if result = decode[ruleTestResult](t, got); result.State != "absent" {
		t.Fatalf("result = %+v", result)
	}
	for _, body := range []map[string]any{
		{"technology": plain, "folder": "relative"},
		{"technology": plain},
		{"technology": plain, "files": []sampleFile{{Path: "../x"}}},
		{"technology": raw(`{}`), "folder": folder},
	} {
		wantStatus(t, h.api("/api/rules/test", body), http.StatusBadRequest)
	}
}

func TestApplyWritesCanonicalFilesAndClearsDraft(t *testing.T) {
	h, root := fixture(t, officialID, false)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	wantStatus(t, h.api("/api/apply", map[string]any{}), http.StatusOK)
	if _, err := registrygen.Run(root, registrygen.Options{Check: true}); err != nil {
		t.Fatalf("output must be canonical: %v", err)
	}
	state := decode[State](t, h.api("/api/state", nil))
	if len(state.Ops) != 0 {
		t.Fatalf("ops = %+v", state.Ops)
	}
	data, err := storage.Read(root, "skills.json", maxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(data), `"tags"`) > strings.Index(string(data), `"quality"`) {
		t.Fatalf("tags written after projected fields:\n%s", data)
	}
	wantStatus(t, h.api("/api/apply", map[string]any{}), http.StatusBadRequest)
}

func TestApplyRefusesValidationErrors(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(strings.Replace(skillJSON(skillA, ""), `["react"]`, `["missing"]`, 1))}), http.StatusOK)
	got := h.api("/api/apply", map[string]any{})
	wantStatus(t, got, http.StatusBadRequest)
	if !strings.Contains(got.Body.String(), "fix validation errors") {
		t.Fatalf("body = %s", got.Body.String())
	}
}

// fakeRunner intercepts registry scripts.
type fakeRunner struct {
	scripts [][]string
	failOn  string
	onRun   func()
}

func (f *fakeRunner) Run(ctx context.Context, _ string, _ []string, name string, args ...string) (process.Output, error) {
	if name != "node" && name != "npm" {
		return process.Output{}, fmt.Errorf("unexpected command %s", name)
	}
	f.scripts = append(f.scripts, append([]string{name}, args...))
	if f.onRun != nil {
		f.onRun()
	}
	if f.failOn != "" && strings.Contains(strings.Join(args, " "), f.failOn) {
		return process.Output{Stderr: []byte("script failed")}, os.ErrInvalid
	}
	return process.Output{}, ctx.Err()
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// #nosec G204 -- tests pass fixed git arguments.
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(key, "Studio Test")
	}
	for _, key := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(key, "studio@example.test")
	}
}

const fixtureOrigin = "https://github.com/o/r.git"

// fakeGitHub answers the pull-request endpoints and counts creations.
type fakeGitHub struct {
	*httptest.Server
	created atomic.Int32
	// failCreate is the status returned for a create until it is reset to zero.
	failCreate atomic.Int32
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	github := &fakeGitHub{}
	var mu sync.Mutex
	open := []map[string]any{}
	github.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode(open)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			if status := github.failCreate.Load(); status != 0 {
				w.WriteHeader(int(status))
				_, _ = w.Write([]byte(`{"message":"denied"}`))
				return
			}
			github.created.Add(1)
			pr := map[string]any{"number": 1, "html_url": "https://example.test/pull/1"}
			open = append(open, pr)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(pr)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(github.Close)
	return github
}

func fixtureCredentials(t *testing.T) *gitclient.Credentials {
	t.Helper()
	home := t.TempDir()
	return &gitclient.Credentials{
		Getenv:  func(name string) string { return map[string]string{"GITHUB_TOKEN": "fixture-token"}[name] },
		HomeDir: func() (string, error) { return home, nil },
		Keyring: func(string, string) (string, error) { return "", os.ErrNotExist },
	}
}

// fixturePolicy dials the fixture's bare repository for the GitHub-shaped origin.
func fixturePolicy(origin string) *gitclient.Policy {
	return &gitclient.Policy{AllowFile: true, AllowHTTP: true, TrustedOrigin: true, Rewrite: func(uri string) string {
		if uri == fixtureOrigin {
			return "file://" + origin
		}
		return uri
	}}
}

type gitFixtureResult struct {
	h        *harness
	root     string
	origin   string
	upstream string
	github   *fakeGitHub
}

// gitFixture returns a registry clone and a second clone acting as upstream. The
// checkout's origin is GitHub-shaped; insteadOf points the Git CLI used by the
// fixture at the bare repository, and the policy's Rewrite does the same for Nosey.
func gitFixture(t *testing.T, id string, runner *fakeRunner, checks bool) (*harness, string, string, string) {
	t.Helper()
	fixture := newGitFixture(t, id, runner, checks)
	return fixture.h, fixture.root, fixture.origin, fixture.upstream
}

func newGitFixture(t *testing.T, id string, runner *fakeRunner, checks bool) gitFixtureResult {
	t.Helper()
	isolateGit(t)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	root := filepath.Join(base, "registry")
	upstream := filepath.Join(base, "upstream")
	gitRun(t, base, "init", "-q", "--bare", "-b", "main", origin)
	gitRun(t, base, "init", "-q", "-b", "main", root)
	writeRegistry(t, root, id)
	gitRun(t, root, "config", "user.name", "Studio Test")
	gitRun(t, root, "config", "user.email", "studio@example.test")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "seed")
	gitRun(t, root, "config", "url."+origin+".insteadOf", fixtureOrigin)
	gitRun(t, root, "remote", "add", "origin", fixtureOrigin)
	gitRun(t, root, "push", "-q", "origin", "main")
	gitRun(t, base, "clone", "-q", origin, upstream)
	github := newFakeGitHub(t)
	opts := Options{Root: root, Runner: runner, RegistryChecks: checks, GitPolicy: fixturePolicy(origin), GitHubBaseURL: github.URL, Credentials: fixtureCredentials(t), TempDir: t.TempDir()}
	return gitFixtureResult{newHarness(t, opts), root, origin, upstream, github}
}

func replaceInFile(t *testing.T, path, from, to string) {
	t.Helper()
	data, err := storage.Read(filepath.Dir(path), filepath.Base(path), maxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(from)) {
		t.Fatalf("%q not found in %s", from, path)
	}
	if err = storage.Write(path, bytes.ReplaceAll(data, []byte(from), []byte(to))); err != nil {
		t.Fatal(err)
	}
}

func commitUpstream(t *testing.T, upstream string) {
	t.Helper()
	if _, err := registrygen.Run(upstream, registrygen.Options{}); err != nil {
		t.Fatal(err)
	}
	gitRun(t, upstream, "add", "-A")
	gitRun(t, upstream, "commit", "-q", "-m", "upstream")
	gitRun(t, upstream, "push", "-q", "origin", "main")
}

func TestPublishReplaysOnRemoteAndLeavesCheckoutAlone(t *testing.T) {
	runner := &fakeRunner{}
	fixture := newGitFixture(t, "x:y", runner, false)
	h, root, origin, upstream := fixture.h, fixture.root, fixture.origin, fixture.upstream
	head := gitRun(t, root, "rev-parse", "HEAD")
	replaceInFile(t, filepath.Join(upstream, "technologies.json"), `"name": "Vue"`, `"name": "Vue.js"`)
	commitUpstream(t, upstream)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	got := h.api("/api/publish", map[string]any{"title": "Tag a skill", "body": "why"})
	wantStatus(t, got, http.StatusOK)
	result := decode[publishResult](t, got)
	if result.Published.URL != "https://example.test/pull/1" || !strings.HasPrefix(result.Published.Branch, "studio/") {
		t.Fatalf("result = %+v", result)
	}
	if gitRun(t, root, "rev-parse", "HEAD") != head || gitRun(t, root, "status", "--porcelain") != "" {
		t.Fatal("publish touched the user's checkout")
	}
	if worktrees := gitRun(t, root, "worktree", "list"); strings.Count(worktrees, "\n") != 0 {
		t.Fatalf("temporary worktree left behind:\n%s", worktrees)
	}
	skills := gitRun(t, origin, "show", result.Published.Branch+":skills.json")
	technologies := gitRun(t, origin, "show", result.Published.Branch+":technologies.json")
	if !strings.Contains(skills, `"tags"`) || !strings.Contains(technologies, "Vue.js") {
		t.Fatalf("branch must hold the edit on top of upstream:\n%s", skills)
	}
	if fixture.github.created.Load() != 1 || len(runner.scripts) != 0 {
		t.Fatalf("created = %d, scripts = %v", fixture.github.created.Load(), runner.scripts)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["y"]`+assessed))}), http.StatusOK)
	got = h.api("/api/publish", map[string]any{"title": "Tag a skill"})
	wantStatus(t, got, http.StatusOK)
	updated := decode[publishResult](t, got)
	if !updated.Updated || updated.Published.Branch != result.Published.Branch || fixture.github.created.Load() != 1 {
		t.Fatalf("second publish should force-update: %+v, created = %d", updated, fixture.github.created.Load())
	}
	if skills = gitRun(t, origin, "show", result.Published.Branch+":skills.json"); !strings.Contains(skills, `"y"`) {
		t.Fatalf("branch not updated:\n%s", skills)
	}
}

func TestPublishSurfacesConflictsAndResolvesThem(t *testing.T) {
	runner := &fakeRunner{}
	h, _, origin, upstream := gitFixture(t, "x:y", runner, false)
	replaceInFile(t, filepath.Join(upstream, "technologies.json"), `"name": "Vue"`, `"name": "Vue upstream"`)
	commitUpstream(t, upstream)
	wantStatus(t, h.stage(change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Vue mine","category":"ui","rules":[{"id":"file-0001","kind":"file","glob":"**/vue.config"}]}`)}), http.StatusOK)
	got := h.api("/api/publish", map[string]any{"title": "Rename"})
	wantStatus(t, got, http.StatusConflict)
	if conflicts := decode[conflictResult](t, got).Conflicts; len(conflicts) != 1 || conflicts[0].ID != "vue" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	wantStatus(t, h.api("/api/draft/resolve", map[string]any{"role": roleTechnologies, "id": "vue", "action": "bad"}), http.StatusBadRequest)
	wantStatus(t, h.api("/api/draft/resolve", map[string]any{"role": roleTechnologies, "id": "vue", "action": "keep"}), http.StatusOK)
	got = h.api("/api/publish", map[string]any{"title": "Rename"})
	wantStatus(t, got, http.StatusOK)
	branch := decode[publishResult](t, got).Published.Branch
	if !strings.Contains(gitRun(t, origin, "show", branch+":technologies.json"), "Vue mine") {
		t.Fatal("kept edit was not published")
	}
}

func TestResolveDiscardDropsConflictedOp(t *testing.T) {
	runner := &fakeRunner{}
	h, _, _, upstream := gitFixture(t, "x:y", runner, false)
	replaceInFile(t, filepath.Join(upstream, "technologies.json"), `"name": "Vue"`, `"name": "Vue upstream"`)
	commitUpstream(t, upstream)
	wantStatus(t, h.stage(change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Mine","category":"ui","rules":[{"id":"file-0001","kind":"file","glob":"**/vue.config"}]}`)}), http.StatusOK)
	wantStatus(t, h.api("/api/publish", map[string]any{"title": "x"}), http.StatusConflict)
	got := h.api("/api/draft/resolve", map[string]any{"role": roleTechnologies, "id": "vue", "action": "discard"})
	wantStatus(t, got, http.StatusOK)
	if result := decode[draftResult](t, got); len(result.Ops) != 0 {
		t.Fatalf("ops = %+v", result.Ops)
	}
	wantStatus(t, h.api("/api/draft/resolve", map[string]any{"role": roleTechnologies, "id": "vue", "action": "keep"}), http.StatusBadRequest)
}

func TestPublishRunsRegistryScriptsInCIOrder(t *testing.T) {
	runner := &fakeRunner{}
	h, root, _, _ := gitFixture(t, officialID, runner, true)
	if err := storage.Write(filepath.Join(root, "tests", "a.test.mjs"), nil); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "tests")
	gitRun(t, root, "push", "-q", "origin", "main")
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillB, "delete": true}), http.StatusOK)
	wantStatus(t, h.api("/api/publish", map[string]any{"title": "Remove"}), http.StatusOK)
	order := []string{}
	for _, script := range runner.scripts {
		order = append(order, strings.Join(script[:min(len(script), 3)], " "))
	}
	want := []string{"node scripts/jev.mjs merge", "node scripts/sync.mjs --write", "npm run --silent", "node --test " + filepath.Dir(filepath.Dir(runner.scripts[3][2])) + "/tests/a.test.mjs"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order = %v", order)
	}
}

func TestFailedRegistryCheckPublishesNothing(t *testing.T) {
	runner := &fakeRunner{failOn: "check"}
	h, _, _, _ := gitFixture(t, officialID, runner, true)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	got := h.api("/api/publish", map[string]any{"title": "x"})
	wantStatus(t, got, http.StatusBadRequest)
	if !strings.Contains(got.Body.String(), "script failed") {
		t.Fatalf("body = %s", got.Body.String())
	}
}

func TestApplyRehearsesRegistryScriptsBeforeWriting(t *testing.T) {
	runner := &fakeRunner{failOn: "check"}
	h, root, _, _ := gitFixture(t, officialID, runner, true)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	wantStatus(t, h.api("/api/apply", map[string]any{}), http.StatusBadRequest)
	if status := gitRun(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("failed rehearsal must leave the tree clean:\n%s", status)
	}
}

func TestPublishNeedsARemoteAndATitle(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	wantStatus(t, h.api("/api/publish", map[string]any{"title": " "}), http.StatusBadRequest)
	got := h.api("/api/publish", map[string]any{"title": "x"})
	wantStatus(t, got, http.StatusBadRequest)
	if !strings.Contains(got.Body.String(), "Apply to working tree") {
		t.Fatalf("body = %s", got.Body.String())
	}
}

func TestClearDropsDraftAndPublishedState(t *testing.T) {
	h, _ := fixture(t, "x:y", false)
	h.server.draft.Published = &Published{Branch: "b"}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	wantStatus(t, h.api("/api/draft/clear", map[string]any{}), http.StatusOK)
	state := decode[State](t, h.api("/api/state", nil))
	if len(state.Ops) != 0 || state.Published != nil {
		t.Fatalf("state = %+v %+v", state.Ops, state.Published)
	}
}

func upstreamSkillRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "uploadpack.allowFilter", "true")
	gitRun(t, dir, "config", "uploadpack.allowAnySHA1InWant", "true")
	files := map[string]string{
		"skills/good/SKILL.md": "---\nname: good\ndescription: \"A good skill\"\n---\nBody\n",
		"skills/odd/SKILL.md":  "---\nname: other-name\n---\nBody\n",
		"a/dupe/SKILL.md":      "---\nname: dupe\n---\n",
		"b/dupe/SKILL.md":      "---\nname: dupe\n---\n",
		"README.md":            "x",
	}
	for name, content := range files {
		if err := storage.Write(filepath.Join(dir, name), []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "skills")
	return dir
}

func TestResolveSkillAppliesLocateRules(t *testing.T) {
	upstream := upstreamSkillRepo(t)
	root := t.TempDir()
	writeRegistry(t, root, officialID)
	h := newHarness(t, Options{Root: root, Credentials: fixtureCredentials(t), GitPolicy: &gitclient.Policy{AllowFile: true, Rewrite: func(string) string { return "file://" + upstream }}})
	uri := "https://github.com/owner/repo"
	got := h.api("/api/skills/resolve", map[string]any{"uri": uri + ".git", "skillName": "good"})
	wantStatus(t, got, http.StatusOK)
	result := decode[Resolution](t, got)
	if result.Repository != "owner/repo" || !fullCommit.MatchString(result.Commit) || len(result.Skills) != 4 {
		t.Fatalf("result = %+v", result)
	}
	if result.Selected == nil || !result.Selected.Trackable || result.Selected.Description != "A good skill" {
		t.Fatalf("selected = %+v", result.Selected)
	}
	for _, skill := range result.Skills {
		if skill.Name == "dupe" && skill.Trackable {
			t.Fatalf("duplicate directory must be untrackable: %+v", skill)
		}
	}
	got = h.api("/api/skills/resolve", map[string]any{"uri": uri, "skillName": "odd"})
	wantStatus(t, got, http.StatusOK)
	if selected := decode[Resolution](t, got).Selected; selected == nil || selected.Trackable || !strings.Contains(selected.Reason, "other-name") {
		t.Fatalf("frontmatter mismatch must be untrackable: %+v", selected)
	}
	wantStatus(t, h.api("/api/skills/resolve", map[string]any{"uri": uri, "skillName": "absent"}), http.StatusBadRequest)
	wantStatus(t, h.api("/api/skills/resolve", map[string]any{"uri": "https://gitlab.com/a/b"}), http.StatusBadRequest)
	wantStatus(t, h.api("/api/skills/resolve", map[string]any{"uri": "owner/repo"}), http.StatusBadRequest)
}

func TestTrackableSkillsRootAndUnsafePaths(t *testing.T) {
	found := trackableSkills([]string{"SKILL.md", "bad path/x/SKILL.md"}, "repo")
	if len(found) != 1 || found[0].Name != "repo" || !found[0].Trackable {
		t.Fatalf("found = %+v", found)
	}
	if name, description := frontmatterFields("no frontmatter"); name != "" || description != "" {
		t.Fatalf("got %q %q", name, description)
	}
}

// TestOfficialRegistryRoundTrip proves a no-op pass through the studio's document
// model reproduces the committed registry exactly.
func TestOfficialRegistryRoundTrip(t *testing.T) {
	source := filepath.Join("..", "..", "registries", "default")
	if _, err := os.Stat(filepath.Join(source, registrygen.ConfigName)); err != nil {
		t.Skip("registry submodule is not checked out")
	}
	cfg, err := registrygen.Load(source)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := loadDocuments(source, cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := overrides(docs)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := registrygen.Stage(source, data)
	if err != nil {
		t.Fatal(err)
	}
	if hasErrors(staged.Diagnostics) {
		t.Fatalf("diagnostics = %+v", staged.Diagnostics)
	}
	for name, want := range staged.Files {
		got, readErr := storage.Read(source, name, maxDocumentBytes)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s changed in a no-op round trip", name)
		}
	}
	if data, readErr := storage.Read(source, locationsPath, maxDocumentBytes); readErr == nil {
		var document locationsFile
		if err = json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		encoded, encodeErr := encodeLocations(document)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatal("locations writer does not reproduce the registry's formatting")
		}
	}
	h := newHarness(t, Options{Root: source})
	state := decode[State](t, h.api("/api/state", nil))
	if !state.Meta.Automated || hasErrors(state.Diagnostics) {
		t.Fatalf("meta = %+v, diagnostics = %+v", state.Meta, state.Diagnostics)
	}
}

func stateOf(t *testing.T, h *harness) State {
	t.Helper()
	got := h.api("/api/state", nil)
	wantStatus(t, got, http.StatusOK)
	return decode[State](t, got)
}

func technologyName(t *testing.T, state State, id string) string {
	t.Helper()
	for _, entity := range state.Documents[roleTechnologies] {
		var technology struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(entity, &technology); err != nil {
			t.Fatal(err)
		}
		if technology.ID == id {
			return technology.Name
		}
	}
	t.Fatalf("technology %s not found", id)
	return ""
}

func renameUpstream(t *testing.T, upstream, to string) {
	t.Helper()
	data, err := storage.Read(upstream, "technologies.json", maxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	updated := bytes.Replace(data, []byte(`"Vue`), []byte(`"`+to+` Vue`), 1)
	if err = storage.Write(filepath.Join(upstream, "technologies.json"), updated); err != nil {
		t.Fatal(err)
	}
	commitUpstream(t, upstream)
}

func TestSyncShowsRemoteWithoutMovingCheckout(t *testing.T) {
	h, root, _, upstream := gitFixture(t, "x:y", &fakeRunner{}, false)
	h.server.opts.Store.Cache = t.TempDir()
	head := gitRun(t, root, "rev-parse", "HEAD")
	renameUpstream(t, upstream, "Remote")
	if name := technologyName(t, stateOf(t, h), "vue"); name != "Vue" {
		t.Fatalf("before sync name = %q", name)
	}
	source := h.server.Sync(context.Background())
	defer h.server.Close()
	if source.Mode != sourceRemote || source.Ref != "origin/main" || source.Behind != 1 || source.Ahead != 0 || source.Dirty || source.Warning != "" {
		t.Fatalf("source = %+v", source)
	}
	state := stateOf(t, h)
	if name := technologyName(t, state, "vue"); name != "Remote Vue" {
		t.Fatalf("after sync name = %q", name)
	}
	if state.Meta.Source.Commit != gitRun(t, upstream, "rev-parse", "HEAD") {
		t.Fatalf("source = %+v", state.Meta.Source)
	}
	if gitRun(t, root, "rev-parse", "HEAD") != head || gitRun(t, root, "status", "--porcelain") != "" {
		t.Fatal("sync moved or dirtied the checkout")
	}
	if worktrees := gitRun(t, root, "worktree", "list"); strings.Count(worktrees, "\n") != 0 {
		t.Fatalf("sync registered a worktree:\n%s", worktrees)
	}
	view := h.server.exported
	h.server.Close()
	if _, err := os.Stat(view); !os.IsNotExist(err) {
		t.Fatalf("snapshot left behind: %v", err)
	}
}

func TestRefreshPicksUpNewCommitsAndFlagsDraftConflicts(t *testing.T) {
	h, root, _, upstream := gitFixture(t, "x:y", &fakeRunner{}, false)
	h.server.opts.Store.Cache = t.TempDir()
	h.server.Sync(context.Background())
	defer h.server.Close()
	first := h.server.exported
	wantStatus(t, h.stage(change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Mine","category":"ui","rules":[{"id":"file-0001","kind":"file","glob":"**/vue.config"}]}`)}), http.StatusOK)
	renameUpstream(t, upstream, "Remote")
	got := h.api("/api/refresh", payload{})
	wantStatus(t, got, http.StatusOK)
	if source := decode[Source](t, got); source.Commit != gitRun(t, upstream, "rev-parse", "HEAD") {
		t.Fatalf("source = %+v", source)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("previous snapshot left behind: %v", err)
	}
	state := stateOf(t, h)
	if len(state.Ops) != 1 {
		t.Fatalf("draft must survive refresh: %+v", state.Ops)
	}
	flagged := false
	for _, diagnostic := range state.Diagnostics {
		flagged = flagged || (diagnostic.Code == "W_STUDIO_CONFLICT" && diagnostic.Subject == "vue")
	}
	if !flagged {
		t.Fatalf("diagnostics = %+v", state.Diagnostics)
	}
	if gitRun(t, root, "status", "--porcelain") != "" {
		t.Fatal("refresh dirtied the checkout")
	}
}

func TestDraftBaseComesFromRemoteSoPublishDoesNotConflict(t *testing.T) {
	runner := &fakeRunner{}
	h, _, origin, upstream := gitFixture(t, "x:y", runner, false)
	h.server.opts.Store.Cache = t.TempDir()
	renameUpstream(t, upstream, "Remote")
	h.server.Sync(context.Background())
	defer h.server.Close()
	wantStatus(t, h.stage(change{"role": roleTechnologies, "id": "vue", "entity": raw(`{"id":"vue","name":"Mine","category":"ui","rules":[{"id":"file-0001","kind":"file","glob":"**/vue.config"}]}`)}), http.StatusOK)
	got := h.api("/api/publish", payload{"title": "Rename"})
	wantStatus(t, got, http.StatusOK)
	branch := decode[publishResult](t, got).Published.Branch
	if !strings.Contains(gitRun(t, origin, "show", branch+":technologies.json"), "Mine") {
		t.Fatal("edit was not published")
	}
}

func TestSyncFallsBackToCheckoutWithWarning(t *testing.T) {
	plain, _ := fixture(t, "x:y", false)
	if source := plain.server.Sync(context.Background()); source.Mode != sourceCheckout || !strings.Contains(source.Warning, "not a Git checkout") {
		t.Fatalf("non-git source = %+v", source)
	}
	h, root, _, upstream := gitFixture(t, "x:y", &fakeRunner{}, false)
	h.server.opts.Store.Cache = t.TempDir()
	renameUpstream(t, upstream, "Remote")
	gitRun(t, root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	source := h.server.Sync(context.Background())
	if source.Mode != sourceCheckout || !strings.Contains(source.Warning, "Could not fetch origin/main") {
		t.Fatalf("offline source = %+v", source)
	}
	if name := technologyName(t, stateOf(t, h), "vue"); name != "Vue" {
		t.Fatalf("name = %q", name)
	}
	gitRun(t, root, "remote", "remove", "origin")
	if source = h.server.Sync(context.Background()); !strings.Contains(source.Warning, "no origin remote") {
		t.Fatalf("no-remote source = %+v", source)
	}
}

func TestSyncRejectsRemoteThatIsNotARegistry(t *testing.T) {
	h, _, _, upstream := gitFixture(t, "x:y", &fakeRunner{}, false)
	h.server.opts.Store.Cache = t.TempDir()
	gitRun(t, upstream, "rm", "-q", registrygen.ConfigName)
	gitRun(t, upstream, "commit", "-q", "-m", "remove config")
	gitRun(t, upstream, "push", "-q", "origin", "main")
	source := h.server.Sync(context.Background())
	if source.Mode != sourceCheckout || !strings.Contains(source.Warning, "not a valid registry") {
		t.Fatalf("source = %+v", source)
	}
	if h.server.exported != "" || h.server.view != h.server.root {
		t.Fatalf("view = %q exported = %q", h.server.view, h.server.exported)
	}
}

func TestLocalModeEditsCheckoutWithoutFetching(t *testing.T) {
	runner := &fakeRunner{}
	h, _, _, upstream := gitFixture(t, "x:y", runner, false)
	h.server.opts.Local = true
	renameUpstream(t, upstream, "Remote")
	source := h.server.Sync(context.Background())
	if source.Mode != sourceCheckout || source.Warning != "" {
		t.Fatalf("source = %+v", source)
	}
	state := stateOf(t, h)
	if !state.Meta.Local || technologyName(t, state, "vue") != "Vue" {
		t.Fatalf("meta = %+v", state.Meta)
	}
}

func TestSyncReportsLocalWorkHiddenByRemoteView(t *testing.T) {
	h, root, _, _ := gitFixture(t, "x:y", &fakeRunner{}, false)
	h.server.opts.Store.Cache = t.TempDir()
	replaceInFile(t, filepath.Join(root, "technologies.json"), `"name": "Vue"`, `"name": "Local Vue"`)
	gitRun(t, root, "commit", "-qam", "local work")
	if err := storage.Write(filepath.Join(root, "untracked.txt"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	source := h.server.Sync(context.Background())
	defer h.server.Close()
	if source.Ahead != 1 || source.Behind != 0 || !source.Dirty {
		t.Fatalf("source = %+v", source)
	}
	if name := technologyName(t, stateOf(t, h), "vue"); name != "Vue" {
		t.Fatalf("remote view must not include local work: %q", name)
	}
}

func TestApplyFromRemoteViewShowsTheWorkingTree(t *testing.T) {
	h, root, _, upstream := gitFixture(t, "x:y", &fakeRunner{}, false)
	h.server.opts.Store.Cache = t.TempDir()
	renameUpstream(t, upstream, "Remote")
	h.server.Sync(context.Background())
	defer h.server.Close()
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	wantStatus(t, h.api("/api/apply", payload{}), http.StatusOK)
	data, err := storage.Read(root, "skills.json", maxDocumentBytes)
	if err != nil || !bytes.Contains(data, []byte(`"tags"`)) {
		t.Fatalf("not applied: %v", err)
	}
	state := stateOf(t, h)
	if state.Meta.Source.Mode != sourceCheckout || !strings.Contains(state.Meta.Source.Warning, "working tree") {
		t.Fatalf("source = %+v", state.Meta.Source)
	}
	if name := technologyName(t, state, "vue"); name != "Vue" {
		t.Fatalf("name = %q", name)
	}
	wantStatus(t, h.api("/api/refresh", payload{}), http.StatusOK)
	if state = stateOf(t, h); state.Meta.Source.Mode != sourceRemote || technologyName(t, state, "vue") != "Remote Vue" {
		t.Fatalf("refresh should return to the remote: %+v", state.Meta.Source)
	}
}

// twoPublishers shares one origin between a checkout and a second studio session
// holding a persisted lease from an earlier publish.
func twoPublishers(t *testing.T) (gitFixtureResult, *harness) {
	t.Helper()
	first := newGitFixture(t, "x:y", &fakeRunner{}, false)
	second := newHarness(t, Options{Root: first.root, Store: testStore(t), Runner: &fakeRunner{}, GitPolicy: fixturePolicy(first.origin), GitHubBaseURL: first.github.URL, Credentials: fixtureCredentials(t), TempDir: t.TempDir()})
	return first, second
}

func publishEdit(h *harness, tag string) *httptest.ResponseRecorder {
	h.t.Helper()
	wantStatus(h.t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["`+tag+`"]`+assessed))}), http.StatusOK)
	return h.api("/api/publish", payload{"title": "Tag"})
}

func TestPublishLeaseRejectsConcurrentRemoteUpdate(t *testing.T) {
	first, second := twoPublishers(t)
	got := publishEdit(first.h, "one")
	wantStatus(t, got, http.StatusOK)
	published := decode[publishResult](t, got).Published

	// A second session pushes to the same branch, moving the remote past the
	// commit the first session recorded.
	second.server.draft.Published = &Published{Branch: published.Branch, Commit: published.Commit, URL: published.URL, Base: published.Base}
	wantStatus(t, publishEdit(second, "two"), http.StatusOK)
	remote := gitRun(t, first.origin, "rev-parse", published.Branch)
	if remote == published.Commit {
		t.Fatal("fixture did not advance the remote branch")
	}

	stale := publishEdit(first.h, "three")
	wantStatus(t, stale, http.StatusBadRequest)
	if !strings.Contains(stale.Body.String(), "changed on the remote") {
		t.Fatalf("body = %s", stale.Body.String())
	}
	if gitRun(t, first.origin, "rev-parse", published.Branch) != remote {
		t.Fatal("a stale lease overwrote the remote branch")
	}
	if first.h.server.draft.Published.Commit != published.Commit {
		t.Fatalf("a rejected update must not change the recorded commit: %+v", first.h.server.draft.Published)
	}
}

func TestPublishPRFailureRetainsPushedBranch(t *testing.T) {
	fixture := newGitFixture(t, "x:y", &fakeRunner{}, false)
	fixture.github.failCreate.Store(http.StatusForbidden)
	got := publishEdit(fixture.h, "x")
	wantStatus(t, got, http.StatusBadRequest)
	for _, part := range []string{"branch was pushed but a pull request could not be opened", "token"} {
		if !strings.Contains(got.Body.String(), part) {
			t.Fatalf("missing %q in %s", part, got.Body.String())
		}
	}
	published := fixture.h.server.draft.Published
	if published == nil || published.URL != "" || !strings.HasPrefix(published.Branch, "studio/") { //nolint:antislop // Commit and Branch are generated, so only these fields are predictable
		t.Fatalf("published = %+v", published)
	}
	if gitRun(t, fixture.origin, "rev-parse", published.Branch) != published.Commit {
		t.Fatal("the pushed branch was not retained")
	}
	reopened := newHarness(t, Options{Root: fixture.root, Store: fixture.h.server.opts.Store})
	if reopened.server.draft.Published == nil || reopened.server.draft.Published.Branch != published.Branch {
		t.Fatalf("partial success was not persisted: %+v", reopened.server.draft.Published)
	}

	// A retry updates the same branch under the lease and opens one pull request.
	fixture.github.failCreate.Store(0)
	retry := fixture.h.api("/api/publish", payload{"title": "Tag"})
	wantStatus(t, retry, http.StatusOK)
	result := decode[publishResult](t, retry)
	if !result.Updated || result.Published.Branch != published.Branch || result.Published.URL == "" || fixture.github.created.Load() != 1 {
		t.Fatalf("result = %+v, created = %d", result, fixture.github.created.Load())
	}

	// A further retry finds the open pull request instead of creating a duplicate.
	fixture.h.server.draft.Published.URL = ""
	wantStatus(t, publishEdit(fixture.h, "y"), http.StatusOK)
	if fixture.github.created.Load() != 1 {
		t.Fatalf("duplicate pull request: created = %d", fixture.github.created.Load())
	}
}

func TestPublishCleanupOnCancellation(t *testing.T) {
	runner := &fakeRunner{}
	fixture := newGitFixture(t, officialID, runner, true)
	h := fixture.h
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["x"]`+assessed))}), http.StatusOK)
	runner.onRun = cancel
	body, err := json.Marshal(payload{"title": "x"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/publish", bytes.NewReader(body)).WithContext(ctx)
	request.Host = testHost
	request.Header.Set(tokenHeader, h.server.Token())
	recorder := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("a canceled publish must fail: %s", recorder.Body.String())
	}
	entries, err := os.ReadDir(h.server.opts.TempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary repository left behind: %v", entries)
	}
	if out := gitRun(t, fixture.origin, "branch", "--list", "studio/*"); out != "" {
		t.Fatalf("nothing should have been pushed: %q", out)
	}
}

func TestPublishNeedsIdentityBeforePushing(t *testing.T) {
	fixture := newGitFixture(t, "x:y", &fakeRunner{}, false)
	gitRun(t, fixture.root, "config", "--unset", "user.name")
	got := publishEdit(fixture.h, "x")
	wantStatus(t, got, http.StatusBadRequest)
	if !strings.Contains(got.Body.String(), "user.name") {
		t.Fatalf("body = %s", got.Body.String())
	}
	if out := gitRun(t, fixture.origin, "branch", "--list", "studio/*"); out != "" {
		t.Fatalf("nothing should have been pushed: %q", out)
	}
}

func TestLocalPathOriginsAreRefusedNotFatal(t *testing.T) {
	fixture := newGitFixture(t, "x:y", &fakeRunner{}, false)
	fixture.h.server.opts.GitPolicy = nil // The production policy.
	fixture.h.server.opts.Store.Cache = t.TempDir()
	gitRun(t, fixture.root, "remote", "set-url", "origin", fixture.origin)
	source := fixture.h.server.Sync(context.Background())
	if source.Mode != sourceCheckout || !strings.Contains(source.Warning, "Could not fetch origin/main") || !strings.Contains(source.Warning, "not supported") {
		t.Fatalf("source = %+v", source)
	}
	got := publishEdit(fixture.h, "x")
	wantStatus(t, got, http.StatusBadRequest)
	if !strings.Contains(got.Body.String(), "local path and file remotes are not supported") {
		t.Fatalf("body = %s", got.Body.String())
	}
}

func TestSyncExportsPartialCloneFromOrigin(t *testing.T) {
	fixture := newGitFixture(t, "x:y", &fakeRunner{}, false)
	renameUpstream(t, fixture.upstream, "Remote")
	gitRun(t, fixture.origin, "config", "uploadpack.allowFilter", "true")
	gitRun(t, fixture.origin, "config", "uploadpack.allowAnySHA1InWant", "true")
	clone := filepath.Join(t.TempDir(), "partial")
	gitRun(t, filepath.Dir(clone), "clone", "-q", "--filter=blob:none", "--no-checkout", "file://"+fixture.origin, clone)
	gitRun(t, clone, "config", "url."+fixture.origin+".insteadOf", fixtureOrigin)
	gitRun(t, clone, "config", "remote.origin.url", fixtureOrigin)
	if err := storage.Write(filepath.Join(clone, registrygen.ConfigName), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "checkout", "-q", "-f", "main") // Fetches only HEAD's blobs through the promisor.
	replaceInFile(t, filepath.Join(fixture.upstream, "technologies.json"), `"name": "Remote Vue"`, `"name": "Newer Vue"`)
	commitUpstream(t, fixture.upstream)
	h := newHarness(t, Options{Root: clone, Store: testStore(t), GitPolicy: fixturePolicy(fixture.origin), Credentials: fixtureCredentials(t), TempDir: t.TempDir()})
	source := h.server.Sync(context.Background())
	defer h.server.Close()
	if source.Mode != sourceRemote || source.Warning != "" {
		t.Fatalf("source = %+v", source)
	}
	if name := technologyName(t, stateOf(t, h), "vue"); name != "Newer Vue" {
		t.Fatalf("name = %q", name)
	}
}

func TestSyncComparisonMatchesGitOnDirtyCases(t *testing.T) {
	fixture := newGitFixture(t, "x:y", &fakeRunner{}, false)
	h, root := fixture.h, fixture.root
	h.server.opts.Store.Cache = t.TempDir()
	if err := storage.Write(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "ignore")
	gitRun(t, root, "push", "-q", "origin", "main")
	if err := storage.Write(filepath.Join(root, "ignored.txt"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if source := h.server.Sync(context.Background()); source.Dirty || gitRun(t, root, "status", "--porcelain") != "" {
		t.Fatalf("ignored files must not be dirty: %+v", source)
	}
	defer h.server.Close()
	replaceInFile(t, filepath.Join(root, "technologies.json"), `"name": "Vue"`, `"name": "Mine"`)
	if source := h.server.Sync(context.Background()); !source.Dirty || gitRun(t, root, "status", "--porcelain") == "" {
		t.Fatalf("modified files must be dirty: %+v", source)
	}
}

func TestStudioNeverInvokesGit(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name) //nolint:gosec // test fixture under t.TempDir
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{`"git",`, `"gh",`, "exec.Command", "gitEnv"} {
			if strings.Contains(string(data), banned) {
				t.Fatalf("%s uses %s", name, banned)
			}
		}
	}
}

func TestDescribeSource(t *testing.T) {
	cases := map[string]Source{
		"Warning: offline":               {Mode: sourceCheckout, Warning: "offline"},
		"Editing origin/main at abcdef1": {Mode: sourceRemote, Ref: "origin/main", Commit: "abcdef1234"},
		"Editing the local checkout":     {Mode: sourceCheckout},
	}
	for want, source := range cases {
		if got := describeSource(source); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// A repository the adapter cannot fully decode must not read as clean.
func TestSyncWarnsWhenDirtyCheckCannotRun(t *testing.T) {
	fixture := newGitFixture(t, "x:y", &fakeRunner{}, false)
	for _, name := range []string{"sparse-kept/file", "sparse-hidden/file"} {
		if err := storage.Write(filepath.Join(fixture.root, name), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, fixture.root, "add", "-A")
	gitRun(t, fixture.root, "commit", "-q", "-m", "directories for a sparse index")
	gitRun(t, fixture.root, "sparse-checkout", "init", "--cone", "--sparse-index")
	gitRun(t, fixture.root, "sparse-checkout", "set", "sparse-kept")
	if data := gitRun(t, fixture.root, "ls-files", "--sparse", "--stage"); !strings.Contains(data, "040000") {
		t.Skipf("this Git did not write a sparse index:\n%s", data)
	}
	source := fixture.h.server.Sync(context.Background())
	if source.Mode != sourceRemote || source.Dirty || !strings.Contains(source.Warning, "may not be clean") {
		t.Fatalf("%+v", source)
	}
}
