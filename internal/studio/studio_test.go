package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// fakeRunner runs real commands but intercepts gh and registry scripts.
type fakeRunner struct {
	real    process.Runner
	gh      [][]string
	scripts [][]string
	failOn  string
}

func (f *fakeRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	switch name {
	case "gh":
		f.gh = append(f.gh, args)
		return process.Output{Stdout: []byte("https://example.test/pull/1\n")}, nil
	case "node", "npm":
		f.scripts = append(f.scripts, append([]string{name}, args...))
		if f.failOn != "" && strings.Contains(strings.Join(args, " "), f.failOn) {
			return process.Output{Stderr: []byte("script failed")}, os.ErrInvalid
		}
		return process.Output{}, nil
	}
	return f.real.Run(ctx, dir, env, name, args...)
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
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(key, "Studio Test")
	}
	for _, key := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(key, "studio@example.test")
	}
}

// gitFixture returns a registry clone and a second clone acting as upstream.
func gitFixture(t *testing.T, id string, runner *fakeRunner, checks bool) (*harness, string, string, string) {
	t.Helper()
	isolateGit(t)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	root := filepath.Join(base, "registry")
	upstream := filepath.Join(base, "upstream")
	gitRun(t, base, "init", "-q", "--bare", "-b", "main", origin)
	gitRun(t, base, "init", "-q", "-b", "main", root)
	writeRegistry(t, root, id)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "seed")
	gitRun(t, root, "remote", "add", "origin", origin)
	gitRun(t, root, "push", "-q", "origin", "main")
	gitRun(t, base, "clone", "-q", origin, upstream)
	h := newHarness(t, Options{Root: root, Runner: runner, RegistryChecks: checks})
	return h, root, origin, upstream
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
	h, root, origin, upstream := gitFixture(t, "x:y", runner, false)
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
	if len(runner.gh) != 1 || len(runner.scripts) != 0 {
		t.Fatalf("gh = %v, scripts = %v", runner.gh, runner.scripts)
	}
	wantStatus(t, h.stage(change{"role": roleSkills, "id": skillA, "entity": raw(skillJSON(skillA, `,"tags":["y"]`+assessed))}), http.StatusOK)
	got = h.api("/api/publish", map[string]any{"title": "Tag a skill"})
	wantStatus(t, got, http.StatusOK)
	updated := decode[publishResult](t, got)
	if !updated.Updated || updated.Published.Branch != result.Published.Branch || len(runner.gh) != 1 {
		t.Fatalf("second publish should force-update: %+v, gh = %v", updated, runner.gh)
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
	if len(runner.gh) != 0 || !strings.Contains(got.Body.String(), "script failed") {
		t.Fatalf("gh = %v, body = %s", runner.gh, got.Body.String())
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
	h := newHarness(t, Options{Root: root, GitURL: func(string) string { return "file://" + upstream }})
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
