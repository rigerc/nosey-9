package installation

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// installedFromSource installs alpha and beta from the fake o/one repository.
func installedFromSource(t *testing.T) (Service, *fakeFetcher, string) {
	t.Helper()
	s, f, src := twoSkillSource(t)
	if _, err := s.Install(context.Background(), []SourcePlan{plan("o/one", shaA, "copy", []string{"codex"}, "alpha", "beta")}, nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	return s, f, src
}

func TestCheckUpdatesConfirmsCurrentSkillsWithOneFetchPerSource(t *testing.T) {
	s, f, _ := installedFromSource(t)
	got, err := s.CheckUpdates(context.Background())
	if err != nil || !got.AllCurrent || got.UpToDate != 2 || len(got.Names) != 0 || got.Failed != 0 || got.Warning() != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Summary() != "All installed skills are up to date." {
		t.Fatal(got.Summary())
	}
	if fetched := f.fetches(); !slices.Equal(fetched, []string{"o/one#" + shaA}) {
		t.Fatal("entries of one source and ref must share one fetch:", fetched)
	}
}

func TestCheckUpdatesReportsChangedSourceAndLocalEdits(t *testing.T) {
	s, _, src := installedFromSource(t)
	if err := storage.Write(filepath.Join(src, "skills", "alpha", "scripts", "run.sh"), []byte("echo changed\n")); err != nil {
		t.Fatal(err)
	}
	if err := storage.Write(filepath.Join(s.Store.Workspace, ".agents", "skills", "beta", "notes.md"), []byte("my edit")); err != nil {
		t.Fatal(err)
	}
	got, err := s.CheckUpdates(context.Background())
	if err != nil || !slices.Equal(got.Names, []string{"alpha", "beta"}) || got.AllCurrent || got.UpToDate != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(got.Summary(), "2 skills can be updated: alpha, beta") {
		t.Fatal(got.Summary())
	}
	inv := s.Inventory(context.Background(), emptyCatalog()).MarkOutdated(got.Names)
	for _, item := range inv.Skills {
		if item.Status != "unknown" { // untracked in the catalog, so only the check's names matter
			t.Fatal(item)
		}
	}
}

func TestCheckUpdatesIgnoresWhatACopyExcludes(t *testing.T) {
	s, _, src := installedFromSource(t)
	// metadata.json is never copied, so adding it upstream must not look like an update.
	if err := storage.Write(filepath.Join(src, "skills", "alpha", "metadata.json"), []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(src, "skills", "alpha", "alias.md")); err != nil {
		t.Fatal(err)
	}
	got, err := s.CheckUpdates(context.Background())
	// The followed symlink does change the copy, so alpha differs; beta must still be current.
	if err != nil || !slices.Equal(got.Names, []string{"alpha"}) || got.UpToDate != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if err = os.Remove(filepath.Join(src, "skills", "alpha", "alias.md")); err != nil {
		t.Fatal(err)
	}
	if got, err = s.CheckUpdates(context.Background()); err != nil || !got.AllCurrent {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCheckUpdatesFetchesEachSourceAndRef(t *testing.T) {
	s, f, src := installedFromSource(t)
	other := t.TempDir()
	writeSkill(t, other, "gamma", "gamma")
	f.dirs["o/one#"+shaB] = src
	f.dirs["o/two#"] = other
	if _, err := s.Install(context.Background(), []SourcePlan{
		plan("o/two", "", "copy", []string{"codex"}, "gamma"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	// A second ref of the same repository is its own group.
	lock := readLock(t, s)
	entry := lock.Skills["beta"]
	entry.Ref = shaB
	lock.Set("beta", entry)
	if err := lock.Write(s.Store.Workspace); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	got, err := s.CheckUpdates(context.Background())
	if err != nil || got.UpToDate != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	fetched := f.fetches()
	slices.Sort(fetched)
	if want := []string{"o/one#" + shaA, "o/one#" + shaB, "o/two#"}; !slices.Equal(fetched, want) {
		t.Fatal(fetched)
	}
}

func TestCheckUpdatesNeverReportsAFailedFetchAsCurrent(t *testing.T) {
	s, f, _ := installedFromSource(t)
	f.errs = map[string]error{"o/one#" + shaA: os.ErrDeadlineExceeded}
	got, err := s.CheckUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Failed != 2 || len(got.Failures) != 1 || !strings.Contains(got.Failures[0], "o/one@"+shaA) || got.AllCurrent || got.UpToDate != 0 || got.NothingToCompare {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Warning(), "could not be checked") || strings.Contains(got.Summary(), "up to date") {
		t.Fatal(got.Warning(), "|", got.Summary())
	}
}

func TestCheckUpdatesReportsLocalAndLockProblemsPerSkill(t *testing.T) {
	s, _, src := installedFromSource(t)
	if err := os.RemoveAll(filepath.Join(src, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(s.Store.Workspace, ".agents", "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	got, err := s.CheckUpdates(context.Background())
	if err != nil || got.Failed != 2 || got.AllCurrent || len(got.Names) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	joined := strings.Join(got.Failures, "\n")
	for _, want := range []string{"alpha", "not installed", "beta", "no longer exists in the source"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
}

func TestCheckUpdatesRejectsUntrustedSkillPaths(t *testing.T) {
	s, _, _ := installedFromSource(t)
	lock := readLock(t, s)
	for name, path := range map[string]string{"alpha": "../outside/SKILL.md", "beta": "skills/beta/README.md"} {
		entry := lock.Skills[name]
		entry.SkillPath = path
		lock.Set(name, entry)
	}
	if err := lock.Write(s.Store.Workspace); err != nil {
		t.Fatal(err)
	}
	got, err := s.CheckUpdates(context.Background())
	if err != nil || got.Failed != 2 || got.UpToDate != 0 || !strings.Contains(strings.Join(got.Failures, "\n"), "invalid skillPath") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCheckUpdatesCountsLegacyEntries(t *testing.T) {
	s := newService(t, nil)
	lock := `{"version":1,"skills":{"old":{"source":"o/one","sourceType":"github","computedHash":"h"},"older":{"source":"o/two","sourceType":"github"}}}`
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte(lock)); err != nil {
		t.Fatal(err)
	}
	got, err := s.CheckUpdates(context.Background())
	if err != nil || got.Legacy != 2 || !got.NothingToCompare || got.AllCurrent {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(got.Summary(), "No installed skills can be compared") || !strings.Contains(got.Warning(), "2 skills cannot be checked") {
		t.Fatal(got.Summary(), got.Warning())
	}
	if fetched := s.Fetcher.(*fakeFetcher).fetches(); len(fetched) != 0 {
		t.Fatal("legacy entries have nothing to fetch:", fetched)
	}
}

func TestCheckUpdatesLockStates(t *testing.T) {
	s := newService(t, nil)
	got, err := s.CheckUpdates(context.Background())
	if err != nil || !got.NothingToCompare || got.Failed != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if err = storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte("<<<<<<< HEAD")); err != nil {
		t.Fatal(err)
	}
	var lockErr skillkit.LockError
	if _, err = s.CheckUpdates(context.Background()); !errorsAs(err, &lockErr) {
		t.Fatal("a broken lock cannot be reported as no updates:", err)
	}
}

func TestCheckUpdatesComparesLocalSourcesInPlace(t *testing.T) {
	registry := t.TempDir()
	skillDir := writeSkill(t, registry, "skills/x", "x", "doc.md", "v1")
	s := newService(t, nil)
	if _, err := s.Install(context.Background(), []SourcePlan{plan(skillDir, "", "copy", []string{"codex"}, "x")}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.CheckUpdates(context.Background())
	if err != nil || !got.AllCurrent {
		t.Fatalf("%+v %v", got, err)
	}
	if err = storage.Write(filepath.Join(skillDir, "doc.md"), []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if got, err = s.CheckUpdates(context.Background()); err != nil || !slices.Equal(got.Names, []string{"x"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if err = os.RemoveAll(registry); err != nil {
		t.Fatal(err)
	}
	if got, err = s.CheckUpdates(context.Background()); err != nil || got.Failed != 1 || len(got.Names) != 0 {
		t.Fatalf("a missing local source is a failure: %+v %v", got, err)
	}
}

func TestCheckUpdatesStopsWhenCancelled(t *testing.T) {
	s, _, _ := installedFromSource(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CheckUpdates(ctx); err == nil {
		t.Fatal("a cancelled check reported a result")
	}
}

func TestUpdateCheckSummaryClaimsOnlyWhatWasCompared(t *testing.T) {
	for _, test := range []struct {
		name    string
		check   UpdateCheck
		summary string
		warning string
	}{
		{"all current", UpdateCheck{AllCurrent: true, UpToDate: 2}, "All installed skills are up to date.", ""},
		{"some current", UpdateCheck{UpToDate: 3, Failed: 1, Failures: []string{"o/one: boom"}}, "3 skills confirmed up to date.", "o/one: boom"},
		{"nothing", UpdateCheck{NothingToCompare: true}, "No installed skills can be compared with their sources.", ""},
		{"legacy", UpdateCheck{NothingToCompare: true, Legacy: 2}, "No installed skills can be compared", "2 skills cannot be checked"},
		{"updates", UpdateCheck{Names: []string{"a"}, Failed: 2, Failures: []string{"x", "y"}}, "1 skill can be updated: a", "x (and 1 more)"},
		{"unknown", UpdateCheck{}, "reported no updates", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.check.Summary(); !strings.Contains(got, test.summary) {
				t.Fatal(got)
			}
			if got := test.check.Warning(); test.warning == "" && got != "" || !strings.Contains(got, test.warning) {
				t.Fatal(got)
			}
		})
	}
}
