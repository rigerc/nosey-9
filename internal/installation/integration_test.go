package installation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
)

// TestPinnedSourceInstallsRecordsAndChecksUpdates fetches the current official pin from GitHub
// with the native Git client, installs it into a disposable project and checks the lock, the inventory
// status and a native update check. It needs network access, so it is opt-in:
//
//	NOSEY_SKILLKIT_INTEGRATION=1 go test ./internal/installation -run '^TestPinned'
func TestPinnedSourceInstallsRecordsAndChecksUpdates(t *testing.T) {
	if os.Getenv("NOSEY_SKILLKIT_INTEGRATION") != "1" {
		t.Skip("set NOSEY_SKILLKIT_INTEGRATION=1 to fetch the official pin from GitHub")
	}
	const (
		uri  = "https://github.com/vercel-labs/agent-skills"
		pin  = "1a2c79d4e6c4a2283dc2c614b6538cbe1ed5ca62"
		name = "vercel-composition-patterns"
	)
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := Service{
		Store:    settings.Store{Workspace: workspace, Data: t.TempDir(), Cache: t.TempDir()},
		Settings: settings.Defaults(),
		Home:     t.TempDir(),
	}
	catalogFor := func(ref string) domain.Catalog {
		return domain.Catalog{Skills: map[string]domain.Skill{"s": {ID: "s", Source: domain.Source{Kind: "git", URI: uri, Ref: ref, SkillName: name}}}}
	}
	plans, err := s.Build(catalogFor(pin), catalog.Recommendations{}, Options{IDs: []string{"s"}, Agents: []string{"claude-code"}, Method: "copy", Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	results, err := s.Install(context.Background(), plans, nil)
	if err != nil {
		t.Fatal(err, results)
	}
	lock := readLock(t, s)
	entry := lock.Skills[name]
	if entry.Ref != pin || entry.SkillPath == "" || entry.ComputedHash == "" || !strings.Contains(entry.Source, "vercel-labs/agent-skills") {
		t.Fatalf("lock did not record the pinned commit and provenance: %+v", entry)
	}
	status := func(ref string) string {
		for _, item := range s.Inventory(context.Background(), catalogFor(ref)).Skills {
			if item.Name == name {
				return item.Status
			}
		}
		return ""
	}
	if got := status(pin); got != "installed" {
		t.Fatal(got)
	}
	if got := status(strings.Repeat("b", 40)); got != "outdated" {
		t.Fatal(got)
	}
	check, err := s.CheckUpdates(context.Background())
	if err != nil || !check.AllCurrent || check.Failed != 0 {
		t.Fatalf("a fresh install must be current: %+v %v", check, err)
	}
}
