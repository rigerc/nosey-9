package installation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

// twoSkillSource prepares a fake repository with skills alpha and beta and returns the service.
func twoSkillSource(t *testing.T) (Service, *fakeFetcher, string) {
	t.Helper()
	src := t.TempDir()
	writeSkill(t, src, "skills/alpha", "alpha", "scripts/run.sh", "echo alpha\n")
	writeSkill(t, src, "skills/beta", "beta")
	f := &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: src}}
	return newService(t, f), f, src
}

func TestRunSourceInstallsAndRecordsProvenance(t *testing.T) {
	s, f, src := twoSkillSource(t)
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"codex", "claude-code"}, "alpha", "beta"))
	if err != nil {
		t.Fatal(err, res.Lines())
	}
	if !res.Started || !res.LockWritten || res.Err != nil || res.Commit == "" || !slices.Equal(res.Installed(), []string{"alpha", "beta"}) {
		t.Fatalf("%+v", res)
	}
	for _, skill := range res.Skills {
		if skill.Status != skillkit.StatusInstalled || len(skill.Agents) != 2 {
			t.Fatalf("%+v", skill)
		}
	}
	for _, dir := range []string{".agents/skills/alpha/scripts/run.sh", ".claude/skills/alpha/SKILL.md", ".agents/skills/beta/SKILL.md"} {
		if !exists(filepath.Join(s.Store.Workspace, filepath.FromSlash(dir))) {
			t.Fatal("missing", dir)
		}
	}
	hash, err := skillkit.FolderHash(filepath.Join(src, "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	entry := readLock(t, s).Skills["alpha"]
	want := skillkit.LockEntry{Source: "o/one", Ref: shaA, SourceType: "github", SkillPath: "skills/alpha/SKILL.md", ComputedHash: hash}
	if entry.Source != want.Source || entry.Ref != want.Ref || entry.SourceType != want.SourceType || entry.SkillPath != want.SkillPath || entry.ComputedHash != want.ComputedHash {
		t.Fatalf("%+v", entry)
	}
	if got := f.fetches(); !slices.Equal(got, []string{"o/one#" + shaA}) {
		t.Fatal("a source must be fetched exactly once:", got)
	}
	if _, err = os.Stat(filepath.Join(s.Store.Workspace, ".nosey")); !os.IsNotExist(err) {
		t.Fatal("install wrote nosey project state", err)
	}
}

func TestRunSourceReportsPartialFailureAndRecordsWhatInstalled(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "blocked", Name: "Blocked", SkillsDir: ".blocked/skills"}}
	// A regular file where the agent's directory belongs makes only that agent fail.
	if err := os.WriteFile(filepath.Join(s.Store.Workspace, ".blocked"), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"blocked", "codex"}, "alpha", "beta"))
	if err == nil || res.Err == nil {
		t.Fatal("a failed agent must fail the run")
	}
	if len(res.Skills) != 2 {
		t.Fatalf("%+v", res.Skills)
	}
	for _, skill := range res.Skills {
		if skill.Status != skillkit.StatusInstalled {
			t.Fatalf("a skill that reached one agent is installed: %+v", skill)
		}
		outcomes := map[string]skillkit.Status{}
		for _, agent := range skill.Agents {
			outcomes[agent.Agent] = agent.Status
		}
		if outcomes["codex"] != skillkit.StatusInstalled || outcomes["blocked"] != skillkit.StatusFailed {
			t.Fatalf("%v", outcomes)
		}
	}
	if !res.LockWritten {
		t.Fatal("completed work was not recorded")
	}
	lock := readLock(t, s)
	if len(lock.Skills) != 2 || lock.Skills["alpha"].SkillPath == "" {
		t.Fatalf("%+v", lock.Skills)
	}
	if lines := strings.Join(res.Lines(), "\n"); !strings.Contains(lines, "installed alpha for codex") || !strings.Contains(lines, "failed for blocked") {
		t.Fatal(lines)
	}
}

// cancelWhen reports cancellation as soon as a path exists, which makes cancellation land
// between two skills without timing.
type cancelWhen struct {
	context.Context
	path string
}

func (c cancelWhen) Err() error {
	if exists(c.path) {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestRunSourceRecordsCompletedSkillsWhenCancelled(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	op := begin(t, s)
	ctx := cancelWhen{Context: context.Background(), path: filepath.Join(s.Store.Workspace, ".agents", "skills", "alpha")}
	res, err := op.RunSource(ctx, plan("o/one", shaA, "copy", []string{"codex"}, "alpha", "beta"))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	statuses := map[string]skillkit.Status{}
	for _, skill := range res.Skills {
		statuses[skill.Skill] = skill.Status
	}
	if statuses["alpha"] != skillkit.StatusInstalled || statuses["beta"] != skillkit.StatusNotStarted {
		t.Fatalf("%v", statuses)
	}
	lock := readLock(t, s)
	if _, ok := lock.Skills["alpha"]; !ok || len(lock.Skills) != 1 || !res.LockWritten {
		t.Fatalf("cancelled work must be recorded: %+v", lock.Skills)
	}
	if exists(filepath.Join(s.Store.Workspace, ".agents", "skills", "beta")) {
		t.Fatal("a skill after cancellation was installed")
	}
}

func TestRunSourceCancelledBeforeAnythingStartsWritesNoLock(t *testing.T) {
	s, f, _ := twoSkillSource(t)
	op := begin(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := op.RunSource(ctx, plan("o/one", shaA, "copy", []string{"codex"}, "alpha"))
	if !errors.Is(err, context.Canceled) || res.LockWritten || len(f.fetches()) != 0 {
		t.Fatal(err, res.LockWritten, f.fetches())
	}
	if res.Skills[0].Status != skillkit.StatusNotStarted || res.Skills[0].Agents[0].Status != skillkit.StatusNotStarted {
		t.Fatalf("%+v", res.Skills)
	}
	if exists(filepath.Join(s.Store.Workspace, "skills-lock.json")) {
		t.Fatal("a lock was created for an install that never started")
	}
}

func TestMissingRequestedSkillInstallsNothing(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"codex"}, "alpha", "absent"))
	if err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatal(err)
	}
	if res.LockWritten || len(res.Installed()) != 0 || exists(filepath.Join(s.Store.Workspace, "skills-lock.json")) {
		t.Fatalf("%+v", res)
	}
}

func TestFetchFailureIsStructured(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan("o/missing", shaB, "copy", []string{"codex"}, "alpha"))
	var fetchErr *skillkit.FetchError
	if !errors.As(err, &fetchErr) || !res.Started || res.Skills[0].Status != skillkit.StatusNotStarted {
		t.Fatal(err, res)
	}
}

func TestFailedLockWriteIsAnIncompleteOperation(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	op := begin(t, s)
	// The lock became unusable after the final preflight: a directory sits where the file belongs.
	if err := os.Mkdir(filepath.Join(s.Store.Workspace, "skills-lock.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	res, err := op.RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"codex"}, "alpha"))
	if err == nil || !strings.Contains(err.Error(), "skills-lock.json could not be updated") || res.LockWritten {
		t.Fatal("an unrecorded install was reported as success:", err, res.LockWritten)
	}
	if !exists(filepath.Join(s.Store.Workspace, ".agents", "skills", "alpha", "SKILL.md")) {
		t.Fatal("the confirmed install must stay on disk and be reported")
	}
	if len(res.Installed()) != 1 {
		t.Fatalf("%+v", res.Skills)
	}
	var policy PolicyError
	if errors.As(err, &policy) {
		t.Fatal("an incomplete operation is an install failure, not a policy error")
	}
}

func TestInstallStopsAtTheFirstFailedSource(t *testing.T) {
	s, f, _ := twoSkillSource(t)
	other := t.TempDir()
	writeSkill(t, other, "gamma", "gamma")
	f.dirs["o/three#"] = other
	plans := []SourcePlan{
		plan("o/one", shaA, "copy", []string{"codex"}, "alpha"),
		plan("o/broken", shaB, "copy", []string{"codex"}, "ghost"),
		plan("o/three", "", "copy", []string{"codex"}, "gamma"),
	}
	var events []string
	results, err := s.Install(context.Background(), plans, func(p Progress) {
		state := "start"
		if p.Result != nil {
			state = "done"
		}
		events = append(events, fmt.Sprintf("%d/%d %s", p.Index+1, p.Total, state))
	})
	if err == nil || !strings.Contains(err.Error(), "o/broken") || !strings.Contains(err.Error(), "1 later sources not started") {
		t.Fatal(err)
	}
	if len(results) != 3 || !results[0].Started || !results[1].Started || results[2].Started {
		t.Fatalf("%+v", results)
	}
	if results[2].Skills[0].Status != skillkit.StatusNotStarted {
		t.Fatalf("%+v", results[2])
	}
	if want := []string{"1/3 start", "1/3 done", "2/3 start", "2/3 done"}; !slices.Equal(events, want) {
		t.Fatal(events)
	}
	lock := readLock(t, s)
	if _, ok := lock.Skills["alpha"]; !ok || len(lock.Skills) != 1 {
		t.Fatal("the completed first source must be recorded:", lock.Skills)
	}
	if slices.Contains(f.fetches(), "o/three#") {
		t.Fatal("a later source was fetched")
	}
}

func TestInstallRunsEverySourceInOrder(t *testing.T) {
	s, f, _ := twoSkillSource(t)
	other := t.TempDir()
	writeSkill(t, other, "gamma", "gamma")
	f.dirs["o/two#"+shaB] = other
	results, err := s.Install(context.Background(), []SourcePlan{
		plan("o/one", shaA, "copy", []string{"codex"}, "alpha", "beta"),
		plan("o/two", shaB, "copy", []string{"codex"}, "gamma"),
	}, nil)
	if err != nil || len(results) != 2 {
		t.Fatal(err)
	}
	if got := readLock(t, s).Names(); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatal(got)
	}
	if got := f.fetches(); !slices.Equal(got, []string{"o/one#" + shaA, "o/two#" + shaB}) {
		t.Fatal(got)
	}
}

func TestSkippedAgentsAndWarningsAreSurfaced(t *testing.T) {
	src := t.TempDir()
	writeSkill(t, src, "skills/alpha", "alpha")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "skills", "alpha", "escape.txt")); err != nil {
		t.Fatal(err)
	}
	s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#": src}})
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan("o/one", "", "symlink", []string{"codex", "windsurf"}, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(res.Notes(), "\n")
	for _, want := range []string{"skipped windsurf for alpha", "project directory does not exist", "escape.txt"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("missing %q in\n%s", want, notes)
		}
	}
	if exists(filepath.Join(s.Store.Workspace, ".windsurf")) {
		t.Fatal("a skipped agent's directory was created")
	}
	if exists(filepath.Join(s.Store.Workspace, ".agents", "skills", "alpha", "escape.txt")) {
		t.Fatal("an escaping link was copied")
	}
	if got := strings.Join(res.Lines(), "\n"); !strings.Contains(got, "skipped windsurf") {
		t.Fatal(got)
	}
}

func TestEveryAgentSkippedIsNotASuccess(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan("o/one", shaA, "symlink", []string{"windsurf"}, "alpha"))
	if err == nil || !strings.Contains(err.Error(), "every selected agent was skipped") || res.LockWritten || len(res.Installed()) != 0 {
		t.Fatal(err, res)
	}
	if exists(filepath.Join(s.Store.Workspace, "skills-lock.json")) {
		t.Fatal("a skill that reached no agent was recorded")
	}
}

func TestInternalSkillsAreInstalledWhenNamed(t *testing.T) {
	src := t.TempDir()
	body := "---\nname: hidden\ndescription: Internal fixture.\nmetadata:\n  internal: true\n---\n\nHidden.\n"
	if err := os.MkdirAll(filepath.Join(src, "skills", "hidden"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "hidden", "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#": src}})
	if _, err := begin(t, s).RunSource(context.Background(), plan("o/one", "", "copy", []string{"codex"}, "hidden")); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(s.Store.Workspace, ".agents", "skills", "hidden", "SKILL.md")) {
		t.Fatal("an internal skill named in the plan was not installed")
	}
}

func TestDiscoveryProblemsOnlyFailRequestedSkills(t *testing.T) {
	t.Run("unparsable skill elsewhere is a warning", func(t *testing.T) {
		src := t.TempDir()
		writeSkill(t, src, "skills/good", "good")
		if err := os.MkdirAll(filepath.Join(src, "skills", "bad"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "skills", "bad", "SKILL.md"), []byte("no frontmatter"), 0o600); err != nil {
			t.Fatal(err)
		}
		s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#": src}})
		res, err := begin(t, s).RunSource(context.Background(), plan("o/one", "", "copy", []string{"codex"}, "good"))
		if err != nil || !strings.Contains(strings.Join(res.Notes(), "\n"), "skills/bad/SKILL.md") {
			t.Fatal(err, res.Notes())
		}
	})
	t.Run("a requested skill that cannot be parsed names the problem", func(t *testing.T) {
		src := t.TempDir()
		writeSkill(t, src, "skills/good", "good")
		if err := os.MkdirAll(filepath.Join(src, "skills", "bad"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "skills", "bad", "SKILL.md"), []byte("no frontmatter"), 0o600); err != nil {
			t.Fatal(err)
		}
		s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#": src}})
		_, err := begin(t, s).RunSource(context.Background(), plan("o/one", "", "copy", []string{"codex"}, "good", "bad"))
		if err == nil || !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "could not be parsed") {
			t.Fatal(err)
		}
	})
	t.Run("identical duplicate uses the first with a warning", func(t *testing.T) {
		src := t.TempDir()
		writeSkill(t, src, "skills/dup", "dup", "a.txt", "same")
		writeSkill(t, src, "other/dup", "dup", "a.txt", "same")
		s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#": src}})
		res, err := begin(t, s).RunSource(context.Background(), plan("o/one", "", "copy", []string{"codex"}, "dup"))
		if err != nil || !strings.Contains(strings.Join(res.Notes(), "\n"), "identical content") {
			t.Fatal(err, res.Notes())
		}
		if got := readLock(t, s).Skills["dup"].SkillPath; got != "skills/dup/SKILL.md" {
			t.Fatal("the first definition must be used:", got)
		}
	})
	t.Run("differing duplicate is an error", func(t *testing.T) {
		src := t.TempDir()
		writeSkill(t, src, "skills/dup", "dup", "a.txt", "one")
		writeSkill(t, src, "other/dup", "dup", "a.txt", "two")
		writeSkill(t, src, "skills/fine", "fine")
		s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#": src}})
		op := begin(t, s)
		res, err := op.RunSource(context.Background(), plan("o/one", "", "copy", []string{"codex"}, "dup"))
		if err == nil || !strings.Contains(err.Error(), "different content") || res.LockWritten {
			t.Fatal(err)
		}
		// The same ambiguity does not matter when nobody asked for the duplicated name.
		if _, err = op.RunSource(context.Background(), plan("o/one", "", "copy", []string{"codex"}, "fine")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLocalSourceLockEntryIsPortable(t *testing.T) {
	registry := t.TempDir()
	skillDir := writeSkill(t, registry, "skills/x", "x")
	s := newService(t, nil)
	op := begin(t, s)
	res, err := op.RunSource(context.Background(), plan(skillDir, "", "copy", []string{"codex"}, "x"))
	if err != nil {
		t.Fatal(err, res.Lines())
	}
	raw, err := os.ReadFile(filepath.Join(s.Store.Workspace, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), registry) {
		t.Fatalf("a machine-specific path was written to a committed file:\n%s", raw)
	}
	entry := readLock(t, s).Skills["x"]
	if entry.SourceType != "local" || entry.Source != skillDir || entry.SkillPath != "SKILL.md" {
		t.Fatalf("%+v", entry)
	}
}

func TestExistingSkills176LockIsPreserved(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	fixture, err := os.ReadFile(filepath.Join("..", "skillkit", "testdata", "lock", "skills@1.7.6.json"))
	if err != nil {
		t.Fatal(err)
	}
	withFuture := strings.Replace(string(fixture), `"version": 1,`, `"version": 1,`+"\n"+`  "x-future": {"keep": true},`, 1)
	withFuture = strings.Replace(withFuture, `"sourceType": "git",`, `"sourceType": "git",`+"\n"+`      "x-entry": "kept",`, 1)
	// #nosec G703 -- writes the lock fixture inside the temporary workspace.
	if err = os.WriteFile(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte(withFuture), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readLock(t, s)
	if _, err = begin(t, s).RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"codex"}, "alpha")); err != nil {
		t.Fatal(err)
	}
	after := readLock(t, s)
	if !reflect.DeepEqual(before.Skills["beta"], after.Skills["beta"]) {
		t.Fatalf("an untouched 1.7.6 entry changed:\n%+v\n%+v", before.Skills["beta"], after.Skills["beta"])
	}
	if after.Skills["alpha"].Ref != shaA || after.Skills["alpha"].SourceType != "github" {
		t.Fatalf("the reinstalled entry was not refreshed: %+v", after.Skills["alpha"])
	}
	raw, err := os.ReadFile(filepath.Join(s.Store.Workspace, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"x-future"`, `"x-entry": "kept"`, `"source": "../repo"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("lost %s:\n%s", want, raw)
		}
	}
}

func TestOperationLockExcludesOtherOperations(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	s.LockTimeout = 100 * time.Millisecond
	first := begin(t, s)
	var policy PolicyError
	if _, err := s.Begin(context.Background()); !errors.As(err, &policy) || !strings.Contains(err.Error(), "another Nosey operation is running") {
		t.Fatal("a second operation must be refused while the first holds the lock:", err)
	}
	// A cancelled wait is reported as cancellation, not as a busy workspace.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Begin(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal("Close must be idempotent:", err)
	}
	second, err := s.Begin(context.Background())
	if err != nil {
		t.Fatal("the lock was not released:", err)
	}
	_ = second.Close()
	if _, err = first.RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"codex"}, "alpha")); err == nil {
		t.Fatal("a closed operation ran")
	}
}

func TestConcurrentInstallsAreSerialized(t *testing.T) {
	s, f, src := twoSkillSource(t)
	other := t.TempDir()
	writeSkill(t, other, "gamma", "gamma")
	f.dirs["o/two#"+shaB] = other
	f.onFetch = func(string) { time.Sleep(60 * time.Millisecond) }
	var wg sync.WaitGroup
	errs := make([]error, 2)
	plans := [][]SourcePlan{
		{plan("o/one", shaA, "copy", []string{"codex"}, "alpha")},
		{plan("o/two", shaB, "copy", []string{"codex"}, "gamma")},
	}
	for i := range plans {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Install(context.Background(), plans[i], nil)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	if got := readLock(t, s).Names(); !slices.Equal(got, []string{"alpha", "gamma"}) {
		t.Fatal("a concurrent update was lost:", got, src)
	}
}

// TestHelperHoldOperation is the child of TestOperationLockIsReleasedWhenTheHolderIsKilled.
func TestHelperHoldOperation(t *testing.T) {
	workspace := os.Getenv("NOSEY_TEST_HOLD_WORKSPACE")
	if workspace == "" {
		t.Skip("helper process only")
	}
	s := Service{Store: settings.Store{Workspace: workspace, Data: os.Getenv("NOSEY_TEST_HOLD_DATA")}}
	op, err := s.Begin(context.Background())
	if err != nil {
		fmt.Println("error", err)
		os.Exit(3)
	}
	defer func() { _ = op.Close() }()
	fmt.Println("held")
	time.Sleep(time.Minute)
}

func TestOperationLockIsReleasedWhenTheHolderIsKilled(t *testing.T) {
	s := newService(t, nil)
	s.LockTimeout = 150 * time.Millisecond
	// #nosec G204 G702 -- re-executes this test binary with a fixed test filter.
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldOperation$")
	cmd.Env = append(os.Environ(), "NOSEY_TEST_HOLD_WORKSPACE="+s.Store.Workspace, "NOSEY_TEST_HOLD_DATA="+s.Store.Data)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(out)
	held := false
	for scanner.Scan() {
		if scanner.Text() == "held" {
			held = true
			break
		}
	}
	if !held {
		t.Fatal("the helper never took the lock")
	}
	var policy PolicyError
	if _, err = s.Begin(context.Background()); !errors.As(err, &policy) {
		t.Fatal("another process's lock was ignored:", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	s.LockTimeout = 5 * time.Second
	op, err := s.Begin(context.Background())
	if err != nil {
		t.Fatal("a crashed process left the project locked:", err)
	}
	_ = op.Close()
}

func TestWorkspaceCodeIsNeverExecuted(t *testing.T) {
	src := t.TempDir()
	writeSkill(t, src, "alpha", "alpha")
	s := newService(t, nil)
	marker := filepath.Join(s.Store.Workspace, "MARKER")
	planted := fmt.Sprintf("#!/bin/sh\ntouch %q\n", marker)
	for path, body := range map[string]string{
		"package.json":             `{"name":"victim","scripts":{"preinstall":"touch MARKER","postinstall":"touch MARKER"}}`,
		".npmrc":                   "registry=https://attacker.invalid/\n",
		"git":                      planted,
		"node":                     planted,
		"npx":                      planted,
		"skills":                   planted,
		"node_modules/.bin/git":    planted,
		"node_modules/.bin/node":   planted,
		"node_modules/.bin/skills": planted,
	} {
		full := filepath.Join(s.Store.Workspace, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		// #nosec G306 -- planted executables live in a disposable test project.
		if err := os.WriteFile(full, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(s.Store.Workspace)
	results, err := s.Install(context.Background(), []SourcePlan{plan(filepath.Join(src, "alpha"), "", "copy", []string{"claude-code"}, "alpha")}, nil)
	if err != nil {
		t.Fatal(err, results)
	}
	s.Inventory(context.Background(), emptyCatalog())
	if _, err = s.CheckUpdates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exists(marker) {
		t.Fatal("workspace code was executed")
	}
}

func TestLockRecordsCopyModeLikeSkills176(t *testing.T) {
	s, _, _ := twoSkillSource(t)
	op := begin(t, s)
	if _, err := op.RunSource(context.Background(), plan("o/one", shaA, "copy", []string{"codex"}, "alpha")); err != nil {
		t.Fatal(err)
	}
	raw := func() string {
		data, err := os.ReadFile(filepath.Join(s.Store.Workspace, "skills-lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if !strings.Contains(raw(), `"installMode": "copy"`) {
		t.Fatal(raw())
	}
	if _, err := op.RunSource(context.Background(), plan("o/one", shaA, "symlink", []string{"codex"}, "alpha")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw(), "installMode") {
		t.Fatal("a stale copy mode survived a symlink reinstall:", raw())
	}
}

func TestNotesDropRepeatedWarnings(t *testing.T) {
	res := SourceResult{Skills: []skillkit.InstallResult{{Skill: "a", Warnings: []skillkit.Warning{{Message: "skipped link"}, {Message: "skipped link"}}}}}
	if got := res.Notes(); len(got) != 1 {
		t.Fatal(got)
	}
}
