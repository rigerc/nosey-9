package registrypins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	today   = "2026-11-09"
)

func yes() *bool { value := true; return &value }
func no() *bool  { value := false; return &value }

func TestDecide(t *testing.T) {
	pin := Pin{ID: "x", Commit: commitA}
	held := Pin{ID: "x", Commit: commitA, Quarantined: true}
	cases := []struct {
		name  string
		pin   Pin
		probe Probe
		want  decision
	}{
		{"repo error", pin, Probe{}, decision{action: actionSkip}},
		{"repo gone", pin, Probe{Repo: RepoGone}, decision{action: actionMissing, reason: reasonRepoGone}},
		{"head is pin", pin, Probe{Repo: RepoOK, Head: commitA}, decision{action: actionKeep}},
		{"not inspected", pin, Probe{Repo: RepoOK, Head: commitB}, decision{action: actionSkip}},
		{"absent", pin, Probe{Repo: RepoOK, Head: commitB, Inspected: true}, decision{action: actionMissing, reason: reasonSkillAbsent}},
		{"ambiguous", pin, Probe{Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 2}, decision{action: actionSkip}},
		{"quarantined back at head", held, Probe{Repo: RepoOK, Head: commitA, Inspected: true, HeadHits: 1}, decision{action: actionKeep}},
		{"pin vanished", pin, Probe{Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 1, PinGone: true}, decision{action: actionBump, ref: commitB}},
		{"unknown tree", pin, Probe{Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 1}, decision{action: actionSkip}},
		{"same tree", pin, Probe{Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 1, SameTree: yes()}, decision{action: actionKeep}},
		{"changed tree", pin, Probe{Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 1, SameTree: no()}, decision{action: actionBump, ref: commitB}},
	}
	for _, c := range cases {
		if got := decide(c.pin, c.probe); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

type fakeProber struct {
	probes map[string]Probe
	calls  int
}

func (f *fakeProber) Probe(_ context.Context, _ string, pins []Pin) map[string]Probe {
	f.calls++
	out := map[string]Probe{}
	for _, pin := range pins {
		out[pin.ID] = f.probes[pin.ID]
	}
	return out
}

func skillJSON(id, repo, ref, extra string) string {
	return `{"id":"` + id + `","name":"` + id + `","source":{"kind":"git","uri":"https://github.com/o/` + repo + `","ref":"` + ref + `","skillName":"` + id + `"}` + extra + `}`
}

func fixture(t *testing.T, registryID string, skills ...string) string {
	t.Helper()
	root := t.TempDir()
	config := `{"schemaVersion":1,"registry":{"id":"` + registryID + `","name":"Test"},"manifest":"nosey.registry.json","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"}}`
	files := map[string]string{
		registrygen.ConfigName: config,
		"technologies.json":    `{"schemaVersion":2,"technologies":[]}`,
		"mappings.json":        `{"schemaVersion":2,"mappings":[]}`,
		"skills.json":          `{"schemaVersion":2,"skills":[` + strings.Join(skills, ",") + `]}`,
	}
	for name, data := range files {
		if err := storage.Write(filepath.Join(root, name), []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registrygen.Run(root, registrygen.Options{Command: "generate"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func readSkills(t *testing.T, root string) string {
	t.Helper()
	data, err := storage.Read(root, "skills.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readState(t *testing.T, root string) State {
	t.Helper()
	data, err := storage.Read(root, StateName, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func writeState(t *testing.T, root string, state State) {
	t.Helper()
	if err := storage.JSON(filepath.Join(root, StateName), state); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRepinsPreservingFieldOrderAndRegenerates(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "r", commitA, `,"description":"Zed"`), skillJSON("beta", "r", commitA, ""))
	prober := &fakeProber{probes: map[string]Probe{
		"alpha": {Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 1, SameTree: no()},
		"beta":  {Repo: RepoOK, Head: commitB, Inspected: true, HeadHits: 1, SameTree: yes()},
	}}
	result, err := Update(context.Background(), root, Options{Prober: prober, Today: today})
	if err != nil {
		t.Fatal(err)
	}
	if prober.calls != 1 {
		t.Fatalf("one repository should be probed once, got %d", prober.calls)
	}
	if !slices.Equal(result.Summary.Bumped, []string{"alpha"}) {
		t.Fatal(result.Summary)
	}
	skills := readSkills(t, root)
	if !strings.Contains(skills, `"ref": "`+commitB+`"`) || !strings.Contains(skills, `"ref": "`+commitA+`"`) {
		t.Fatal("only the changed skill should move:", skills)
	}
	if strings.Index(skills, `"source"`) > strings.Index(skills, `"description"`) {
		t.Fatal("field order changed:", skills)
	}
	if _, err = registrygen.Run(root, registrygen.Options{Check: true}); err != nil {
		t.Fatal("registry left out of date:", err)
	}
	if state := readState(t, root); len(state.SeenRepositories) != 1 || state.SeenRepositories[0] != "https://github.com/o/r" {
		t.Fatal(state)
	}
	if !slices.Contains(result.Changed, StateName) || !slices.Contains(result.Changed, "skills.json") {
		t.Fatal(result.Changed)
	}
	if !strings.Contains(result.Summary.Markdown(), "`alpha`") {
		t.Fatal(result.Summary.Markdown())
	}
}

func TestUpdateIgnoresBranchRefsAndWritesNothingWithoutPins(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "r", "main", ""))
	prober := &fakeProber{}
	result, err := Update(context.Background(), root, Options{Prober: prober})
	if err != nil || prober.calls != 0 || len(result.Changed) != 0 {
		t.Fatal(result, err, prober.calls)
	}
	if _, err = os.Stat(filepath.Join(root, StateName)); !os.IsNotExist(err) {
		t.Fatal("state written for a registry with no pins")
	}
	check, err := Check(root)
	if err != nil || check.Pinned != 0 || check.Unpinned != 1 {
		t.Fatal(check, err)
	}
}

func TestUnreadableRepositoryNeverCountsUntilSeen(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "private", commitA, ""))
	gone := &fakeProber{probes: map[string]Probe{"alpha": {Repo: RepoGone}}}
	result, err := Update(context.Background(), root, Options{Prober: gone, Today: today})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Summary.Unreachable, []string{"alpha"}) || len(result.Summary.Strikes) != 0 {
		t.Fatal(result.Summary)
	}
	if !strings.Contains(result.Summary.Markdown(), "Unreachable") {
		t.Fatal(result.Summary.Markdown())
	}
	writeState(t, root, State{SchemaVersion: 1, SeenRepositories: []string{"https://github.com/o/private"}, Probes: map[string]Strike{}})
	result, err = Update(context.Background(), root, Options{Prober: gone, Today: today})
	if err != nil || !slices.Equal(result.Summary.Strikes, []string{"alpha"}) {
		t.Fatal(result.Summary, err)
	}
}

func TestQuarantineNeedsTwoSpacedMissesAndEnforcement(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "r", commitA, ""))
	missing := &fakeProber{probes: map[string]Probe{"alpha": {Repo: RepoOK, Head: commitB, Inspected: true}}}
	run := func(day string, enforce bool) Summary {
		t.Helper()
		result, err := Update(context.Background(), root, Options{Prober: missing, Today: day, Enforce: enforce})
		if err != nil {
			t.Fatal(err)
		}
		return result.Summary
	}
	run("2026-11-01", false)
	if state := readState(t, root); state.Probes["alpha"].Misses != 1 {
		t.Fatal(state)
	}
	run("2026-11-03", false)
	if state := readState(t, root); state.Probes["alpha"].Misses != 1 {
		t.Fatal("misses closer than the gap must not count", state)
	}
	summary := run("2026-11-06", false)
	if !slices.Equal(summary.WouldQuarantine, []string{"alpha"}) || strings.Contains(readSkills(t, root), "quarantine") {
		t.Fatal("report-only run changed the skill", summary)
	}
	if !strings.Contains(summary.Markdown(), "report-only") {
		t.Fatal(summary.Markdown())
	}
	summary = run("2026-11-07", true)
	if !slices.Equal(summary.Quarantined, []string{"alpha"}) || !strings.Contains(readSkills(t, root), `"lastSeenRef": "`+commitA+`"`) {
		t.Fatal(summary, readSkills(t, root))
	}
	if check, err := Check(root); err != nil || check.Quarantined != 1 || len(check.Problems) != 0 {
		t.Fatal(check, err)
	}
	run("2026-11-20", true)
	if !strings.Contains(readSkills(t, root), `"lastChecked": "2026-11-20"`) {
		t.Fatal("enforced probe should refresh lastChecked")
	}
	back := &fakeProber{probes: map[string]Probe{"alpha": {Repo: RepoOK, Head: commitA, Inspected: true, HeadHits: 1}}}
	result, err := Update(context.Background(), root, Options{Prober: back, Today: "2026-11-27", Enforce: true})
	if err != nil || !slices.Equal(result.Summary.Restored, []string{"alpha"}) || strings.Contains(readSkills(t, root), "quarantine") {
		t.Fatal(result.Summary, err)
	}
	if state := readState(t, root); len(state.Probes) != 0 {
		t.Fatal("restored skill kept its history", state)
	}
}

func TestSafetyRailBlocksMassQuarantine(t *testing.T) {
	skills, probes, strikes := []string{}, map[string]Probe{}, map[string]Strike{}
	for _, id := range []string{"a", "b", "c"} {
		skills = append(skills, skillJSON(id, "r", commitA, ""))
		probes[id] = Probe{Repo: RepoOK, Head: commitB, Inspected: true}
		strikes[id] = Strike{Misses: 1, FirstMissAt: "2026-10-01", LastMissAt: "2026-10-01", Reason: reasonSkillAbsent}
	}
	root := fixture(t, "local:test", skills...)
	writeState(t, root, State{SchemaVersion: 1, SeenRepositories: []string{}, Probes: strikes})
	result, err := Update(context.Background(), root, Options{Prober: &fakeProber{probes: probes}, Today: today, Enforce: true})
	if err != nil || !result.Summary.RailTripped || len(result.Summary.Quarantined) != 0 || strings.Contains(readSkills(t, root), "quarantine") {
		t.Fatal(result.Summary, err)
	}
	if !strings.Contains(result.Summary.Markdown(), "Safety rail") {
		t.Fatal(result.Summary.Markdown())
	}
}

func TestDeletedSkillHistoryIsDropped(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "r", commitA, ""))
	writeState(t, root, State{SchemaVersion: 1, SeenRepositories: []string{}, Probes: map[string]Strike{"removed": {Misses: 1, Reason: reasonRepoGone}}})
	prober := &fakeProber{probes: map[string]Probe{"alpha": {Repo: RepoOK, Head: commitA}}}
	if _, err := Update(context.Background(), root, Options{Prober: prober, Today: today}); err != nil {
		t.Fatal(err)
	}
	if state := readState(t, root); len(state.Probes) != 0 {
		t.Fatal(state)
	}
}

func TestUpdateRefusalsAndBadState(t *testing.T) {
	official := fixture(t, officialID, skillJSON("alpha", "r", commitA, ""))
	if _, err := Update(context.Background(), official, Options{Prober: &fakeProber{}}); err == nil {
		t.Fatal("official registry accepted")
	}
	if _, err := Update(context.Background(), t.TempDir(), Options{Prober: &fakeProber{}}); err == nil {
		t.Fatal("missing config accepted")
	}
	root := fixture(t, "local:test", skillJSON("alpha", "r", commitA, ""))
	for _, state := range []string{`{"schemaVersion":9}`, `{"schemaVersion":1,"extra":true}`, `not json`} {
		if err := storage.Write(filepath.Join(root, StateName), []byte(state)); err != nil {
			t.Fatal(err)
		}
		if _, err := Check(root); err == nil {
			t.Fatalf("state %s accepted", state)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	clean := fixture(t, "local:test", skillJSON("alpha", "r", commitA, ""))
	if _, err := Update(canceled, clean, Options{Prober: &fakeProber{}}); err == nil {
		t.Fatal("canceled update wrote results")
	}
}

func TestUpdateTimeoutLeavesRepositoriesForNextRun(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "r", commitA, ""))
	prober := &fakeProber{}
	result, err := Update(context.Background(), root, Options{Prober: prober, Timeout: time.Nanosecond, Today: today})
	if err != nil || !result.Summary.TimedOut || prober.calls != 0 || result.Summary.Probed != 0 {
		t.Fatal(result.Summary, err, prober.calls)
	}
	if !strings.Contains(result.Summary.Markdown(), "time limit") {
		t.Fatal(result.Summary.Markdown())
	}
}

func TestCheckFindsQuarantineWithoutHistory(t *testing.T) {
	root := fixture(t, "local:test", skillJSON("alpha", "r", commitA, `,"quarantine":{"reason":"repo_gone","since":"2026-11-01","lastSeenRef":"`+commitA+`"}`))
	check, err := Check(root)
	if err != nil || len(check.Problems) != 1 {
		t.Fatal(check, err)
	}
}

func TestExpandURIAndKeys(t *testing.T) {
	if got := expandURI("owner/repo"); got != "https://github.com/owner/repo" {
		t.Fatal(got)
	}
	for _, uri := range []string{"https://gitlab.com/a/b", "git@github.com:a/b"} {
		if got := expandURI(uri); got != uri {
			t.Fatal(got)
		}
	}
	if repoKey("https://github.com/Owner/Repo.git") != "https://github.com/owner/repo" {
		t.Fatal(repoKey("https://github.com/Owner/Repo.git"))
	}
	if repositoryName("git@github.com:a/skills.git") != "skills" {
		t.Fatal(repositoryName("git@github.com:a/skills.git"))
	}
	if repoMissing(context.Background(), "https://gitlab.com/a/b") {
		t.Fatal("non-GitHub host confirmed as gone")
	}
}
