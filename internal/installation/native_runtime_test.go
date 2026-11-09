package installation

import (
	"context"
	"errors"
	"net/http/cgi" //nolint:gosec // the test fixture serves git-http-backend; Httpoxy does not apply
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

// TestNativeGitInstallAndUpdateWithoutExecutables installs and updates a skill
// from a Git source with PATH emptied, through git-http-backend started by
// absolute path outside the Nosey process.
func TestNativeGitInstallAndUpdateWithoutExecutables(t *testing.T) {
	originalPath := os.Getenv("PATH")
	root := t.TempDir()
	bare := filepath.Join(root, "skills.git")
	repo := filepath.Join(t.TempDir(), "skills")
	git := func(dir string, args ...string) string {
		t.Helper()
		// #nosec G204 -- test fixture with a fixed binary and test-controlled arguments.
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	backend := filepath.Join(git(root, "--exec-path"), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend is not installed")
	}
	commitSkill := func(version string) string {
		t.Helper()
		writeSkill(t, repo, "skills/alpha", "alpha", "data.txt", version)
		git(repo, "add", "-A")
		git(repo, "commit", "-q", "-m", version)
		git(repo, "push", "-q", bare, "HEAD:refs/heads/main")
		return git(repo, "rev-parse", "HEAD")
	}
	git(root, "init", "-q", "--bare", bare)
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git(repo, "init", "-q", "-b", "main")
	first := commitSkill("v1")
	git(bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	server := httptest.NewServer(&cgi.Handler{Path: backend, Root: "/", Env: []string{
		"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"PATH=" + originalPath,
	}})
	defer server.Close()
	home := t.TempDir()
	tmp := t.TempDir()
	client := gitclient.New(gitclient.Options{
		Policy: gitclient.Policy{AllowHTTP: true, Rewrite: func(string) string { return server.URL + "/skills.git" }},
		Credentials: &gitclient.Credentials{
			Getenv:  func(string) string { return "" },
			HomeDir: func() (string, error) { return home, nil },
			Keyring: func(string, string) (string, error) { return "", errors.New("no keyring") },
		},
		TempDir: tmp,
	})
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := Service{
		Store: settings.Store{Workspace: workspace, Data: t.TempDir(), Cache: t.TempDir()}, Settings: settings.Defaults(),
		Fetcher: skillkit.Fetcher{TempDir: tmp, Client: client}, Home: t.TempDir(),
	}
	catalogFor := func(ref string) domain.Catalog {
		return domain.Catalog{Skills: map[string]domain.Skill{"s": {ID: "s", Source: domain.Source{Kind: "git", URI: "https://github.com/o/r", Ref: ref, SkillName: "alpha"}}}}
	}

	t.Setenv("PATH", "")
	plans, err := s.Build(catalogFor(first), catalog.Recommendations{}, Options{IDs: []string{"s"}, Agents: []string{"claude-code"}, Method: "copy", Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if results, installErr := s.Install(context.Background(), plans, nil); installErr != nil {
		t.Fatal(installErr, results)
	}
	data := filepath.Join(workspace, ".claude", "skills", "alpha", "data.txt")
	if got, _ := os.ReadFile(data); string(got) != "v1" {
		t.Fatalf("installed %q", got)
	}

	t.Setenv("PATH", originalPath)
	second := commitSkill("v2")
	t.Setenv("PATH", "")
	plan, err := s.PlanUpdate(context.Background(), catalogFor(second), catalog.Recommendations{}, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results, updateErr := s.Update(context.Background(), plan, nil); updateErr != nil {
		t.Fatal(updateErr, results)
	}
	if got, _ := os.ReadFile(data); string(got) != "v2" {
		t.Fatalf("updated %q", got)
	}
}
