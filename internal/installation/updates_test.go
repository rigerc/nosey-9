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
	report := "\x1b[38;5;145mChecking for skill updates…\x1b[0m\n\n\x1b[0m↑ zeta has an update\x1b[0m\n↑ alpha has an update\n↑ alpha has an update\n↑ Frontend Design has an update\n3 skill(s) can be updated; run npx skills update to install\n"
	r := &checkRunner{out: process.Output{Stdout: []byte(report)}, err: errors.New("exit status 1")}
	got, err := checkService(r).CheckUpdates(context.Background())
	if err != nil || !reflect.DeepEqual(got.Names, []string{"Frontend Design", "alpha", "zeta"}) || got.Warning() != "" {
		t.Fatal(got, err)
	}
	if want := []string{"--yes", "skills@" + Version, "check", "--project"}; !reflect.DeepEqual(r.args, want) {
		t.Fatal(r.args)
	}
}

func TestCheckUpdatesClaimsOnlyWhatUpstreamCompared(t *testing.T) {
	for _, test := range []struct{ name, stdout, summary, warning string }{
		{"all current", "✓ All project skills are up to date\n", "All installed skills are up to date.", ""},
		{"nothing installed", "No project skills to update.\nInstall project skills with npx skills add <package>\n", "No installed skills can be compared", ""},
		{"legacy only", "No project skills can be updated in place.\n\n2 project skill(s) cannot be updated automatically (installed before skillPath tracking):\n", "No installed skills can be compared", "2 skills cannot be checked"},
		{"some current", "3 project skill(s) already up to date\n", "3 skills confirmed up to date.", ""},
		{"unrecognized report", "something new\n", "reported no updates", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := checkService(&checkRunner{out: process.Output{Stdout: []byte(test.stdout)}}).CheckUpdates(context.Background())
			if err != nil || len(got.Names) != 0 || !strings.Contains(got.Summary(), test.summary) {
				t.Fatal(got.Summary(), err)
			}
			if test.warning == "" && got.Warning() != "" || !strings.Contains(got.Warning(), test.warning) {
				t.Fatal(got.Warning())
			}
		})
	}
}

func TestCheckUpdatesReportsPartialFailure(t *testing.T) {
	stdout := "  ✗ Failed to check for deleted skills from o/two\n↑ alpha has an update\n\nFailed to update 2 skill(s)\n1 skill(s) can be updated; run npx skills update to install\n"
	got, err := checkService(&checkRunner{out: process.Output{Stdout: []byte(stdout)}, err: errors.New("exit status 1")}).CheckUpdates(context.Background())
	if err != nil || !reflect.DeepEqual(got.Names, []string{"alpha"}) || !strings.Contains(got.Warning(), "o/two") {
		t.Fatal(got, got.Warning(), err)
	}
}

func TestCheckUpdatesFailures(t *testing.T) {
	npmNoise := "npm warn exec The following package was not found and will be installed: skills@1.7.6\n"
	for _, test := range []struct {
		name, stdout, stderr, want string
		hidden                     string
	}{
		{name: "source failure beats npm noise", stdout: "✗ Failed to check for deleted skills from o/one\n\nFailed to update 1 skill(s)\n", stderr: npmNoise, want: "Failed to check for deleted skills from o/one", hidden: "npm warn"},
		{name: "real stderr beats npm noise", stderr: npmNoise + "Error: boom\n", want: "Error: boom", hidden: "npm warn"},
		{name: "partial output without a closing summary", stdout: "↑ alpha has an update\n", want: "alpha has an update"},
		{name: "only a count", stdout: "Failed to update 2 skill(s)\n", want: "2 skills failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &checkRunner{out: process.Output{Stdout: []byte(test.stdout), Stderr: []byte(test.stderr)}, err: errors.New("signal: killed")}
			got, err := checkService(r).CheckUpdates(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) || len(got.Names) != 0 {
				t.Fatal(got, err)
			}
			if test.hidden != "" && strings.Contains(err.Error(), test.hidden) {
				t.Fatal("noise hid the cause:", err)
			}
		})
	}
}

func TestParseUpdatesSanitizesReportedText(t *testing.T) {
	got := parseUpdates("↑ bad\x07name has an update\n✗ Failed\x07 here\n")
	if !reflect.DeepEqual(got.Names, []string{"badname"}) || !reflect.DeepEqual(got.Failures, []string{"Failed here"}) {
		t.Fatal(got)
	}
}

func TestMarkOutdatedOnlyFlagsInstalledSkills(t *testing.T) {
	inv := Inventory{Skills: []InventoryItem{{Name: "a", Status: "installed"}, {Name: "b", Status: "unknown"}, {Name: "c", Status: "installed"}}}
	got := inv.MarkOutdated([]string{"a", "b"})
	if got.Skills[0].Status != "outdated" || got.Skills[1].Status != "unknown" || got.Skills[2].Status != "installed" || inv.Skills[0].Status != "installed" {
		t.Fatal(got.Skills, inv.Skills)
	}
}
