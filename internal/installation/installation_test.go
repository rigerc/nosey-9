package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

type recordingRunner struct {
	calls  int
	output process.Output
	err    error
}

func (r *recordingRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	r.calls++
	return r.output, r.err
}

// stubCLI keeps fixtures off the machine's real skills, npx and node.
func stubCLI(context.Context) (CLI, error) {
	return CLI{Name: "npx", Args: []string{"--yes", "skills@" + Version}, Backend: "npx"}, nil
}

func TestNodeVersionRequirementAndUnknownInventory(t *testing.T) {
	for _, version := range []string{"v22.19.9", "v20.20.0", "vgarbage", "22.20"} {
		if supportedNode(version) {
			t.Fatal("unsupported Node accepted", version)
		}
	}
	for _, version := range []string{"v22.20.0", "v23.0.0", "v26.13.0"} {
		if !supportedNode(version) {
			t.Fatal("supported Node rejected", version)
		}
	}
	runner := &recordingRunner{err: fmt.Errorf("unavailable")}
	s := Service{Store: settings.Store{Workspace: t.TempDir()}, Settings: settings.Defaults(), Runner: runner, Resolver: stubCLI}
	inventory := s.Inventory(context.Background(), domain.Catalog{})
	if inventory.Complete || len(inventory.Diagnostics) == 0 {
		t.Fatal("failed inventory claimed complete")
	}
	if s.requirePrerequisites(context.Background()) == nil {
		t.Fatal("missing prerequisites accepted")
	}
	if normalizeSource("https://github.com/Owner/Repo.git", "/") != "owner/repo" || !strings.HasSuffix(normalizeSource("./skill", s.Store.Workspace), "skill") {
		t.Fatal("logical source normalization failed")
	}
	if !sourceMatches("/", "git@host:o/r.git", "o/r", "git@host:o/r.git") || sourceMatches("/", "o/other", "o/r", "") {
		t.Fatal("sourceUrl fallback failed")
	}
}

func TestPrerequisitesReportTheCLIBackend(t *testing.T) {
	s := Service{Store: settings.Store{Workspace: t.TempDir()}, Runner: versionRunner{}, Resolver: stubCLI}
	checks := s.CheckPrerequisites(context.Background())
	if len(checks) != 3 || checks[2].ID != "skills" || checks[2].Status != "pass" || checks[2].Message != Version+" (npx)" {
		t.Fatalf("%+v", checks)
	}
	s.Resolver = func(context.Context) (CLI, error) { return CLI{}, PrerequisiteError{Reason: "not found"} }
	checks = s.CheckPrerequisites(context.Background())
	if checks[2].Status != "fail" || !strings.Contains(checks[2].Message, "not found") {
		t.Fatalf("%+v", checks[2])
	}
}

func TestQuarantinedAndBlockedSkillsAreNeverBuilt(t *testing.T) {
	c := domain.Catalog{Quarantined: map[string]domain.Skill{"gone": {ID: "gone"}}, Blocked: map[string]string{"blocked": "policy"}}
	for _, id := range []string{"gone", "blocked"} {
		_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{id}})
		var policy PolicyError
		if !errors.As(err, &policy) {
			t.Fatal(id, err)
		}
	}
}

func TestLockErrorsNameTheirCause(t *testing.T) {
	s := Service{Store: settings.Store{Workspace: t.TempDir()}}
	write := func(data string) {
		t.Helper()
		if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	for want, data := range map[string]string{
		"merge conflict": "<<<<<<< HEAD\n{}\n=======\n{}\n>>>>>>> x",
		"not valid JSON": "{",
		"version 2":      `{"version":2,"skills":{}}`,
		`"skills" map`:   `{"version":1}`,
	} {
		write(data)
		_, err := s.readLock()
		var lockErr LockError
		if !errors.As(err, &lockErr) || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
	if err := os.Remove(filepath.Join(s.Store.Workspace, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readLock(); !errors.Is(err, errLockMissing) {
		t.Fatal(err)
	}
}

// TestPinnedSkillsVersionIsInSync guards the three places that carry the upstream pin; Dependabot bumps the first.
func TestPinnedSkillsVersionIsInSync(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tools", "skills", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DevDependencies["skills"] != Version || settings.Defaults().Installation.Version != Version {
		t.Fatalf("skills pin out of sync: package.json=%q installation.Version=%q settings default=%q",
			manifest.DevDependencies["skills"], Version, settings.Defaults().Installation.Version)
	}
}
