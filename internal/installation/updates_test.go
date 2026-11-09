package installation

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
)

type checkRunner struct {
	out  process.Output
	err  error
	args []string
}

func (r *checkRunner) Run(_ context.Context, _ string, _ []string, _ string, args ...string) (process.Output, error) {
	r.args = args
	return r.out, r.err
}

func checkService(r *checkRunner) Service {
	return Service{Store: settings.Store{Workspace: "/w"}, Runner: r, Resolver: stubCLI}
}

func TestCheckUpdatesParsesUpstreamReport(t *testing.T) {
	report := "\x1b[38;5;145mChecking for skill updates…\x1b[0m\n\n\x1b[0m↑ zeta has an update\x1b[0m\n↑ alpha has an update\n↑ alpha has an update\n↑ ../evil has an update\n2 skill(s) can be updated\n"
	r := &checkRunner{out: process.Output{Stdout: []byte(report)}, err: errors.New("exit status 1")}
	got, err := checkService(r).CheckUpdates(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatal(got, err)
	}
	if want := []string{"--yes", "skills@" + Version, "check", "--project"}; !reflect.DeepEqual(r.args, want) {
		t.Fatal(r.args)
	}
}

func TestCheckUpdatesNoneAndFailure(t *testing.T) {
	r := &checkRunner{out: process.Output{Stdout: []byte("✓ All project skills are up to date\n")}}
	if got, err := checkService(r).CheckUpdates(context.Background()); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	r = &checkRunner{out: process.Output{Stdout: []byte("✗ Failed to check for deleted skills from o/one\n")}, err: errors.New("exit status 1")}
	if _, err := checkService(r).CheckUpdates(context.Background()); err == nil || !strings.Contains(err.Error(), "Failed to check") {
		t.Fatal("failed check reported as no updates:", err)
	}
}

func TestMarkOutdatedOnlyFlagsInstalledSkills(t *testing.T) {
	inv := Inventory{Skills: []InventoryItem{{Name: "a", Status: "installed"}, {Name: "b", Status: "unknown"}, {Name: "c", Status: "installed"}}}
	got := inv.MarkOutdated([]string{"a", "b"})
	if got.Skills[0].Status != "outdated" || got.Skills[1].Status != "unknown" || got.Skills[2].Status != "installed" || inv.Skills[0].Status != "installed" {
		t.Fatal(got.Skills, inv.Skills)
	}
}
