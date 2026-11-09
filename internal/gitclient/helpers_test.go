package gitclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...) //nolint:gosec // test fixture running git with test-controlled arguments
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@a", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@a",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))     //nolint:gosec // test fixture under t.TempDir
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644)) //nolint:gosec // test fixture under t.TempDir
}

// fixture is a bare origin with three commits on main.
type fixture struct {
	bare    string
	src     string
	commits []string
}

func newFixture(t *testing.T, config ...string) fixture {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(src, 0o755)) //nolint:gosec // test fixture under t.TempDir
	gitCmd(t, src, "init", "-q", "-b", "main")
	f := fixture{src: src, bare: filepath.Join(root, "origin.git")}
	for i := 1; i <= 3; i++ {
		writeFile(t, filepath.Join(src, "skills/a/SKILL.md"), fmt.Sprintf("v%d\n", i))
		writeFile(t, filepath.Join(src, fmt.Sprintf("f%d", i)), "x")
		gitCmd(t, src, "add", "-A")
		gitCmd(t, src, "commit", "-q", "-m", fmt.Sprint(i))
		f.commits = append(f.commits, gitCmd(t, src, "rev-parse", "HEAD"))
	}
	gitCmd(t, src, "tag", "light", f.commits[0])
	gitCmd(t, src, "tag", "-a", "annotated", "-m", "t", f.commits[1])
	gitCmd(t, root, "clone", "-q", "--bare", src, f.bare)
	for _, entry := range config {
		key, value, _ := strings.Cut(entry, "=")
		gitCmd(t, f.bare, "config", key, value)
	}
	return f
}

func (f fixture) url() string { return "file://" + f.bare }

// fixtureClient may use the file transport; only tests can set AllowFile.
func fixtureClient(t *testing.T) *Client {
	t.Helper()
	return New(Options{Policy: Policy{AllowFile: true}, Credentials: emptyCredentials(t.TempDir()), TempDir: t.TempDir()})
}

func emptyCredentials(home string) *Credentials {
	return &Credentials{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return home, nil },
		Keyring: func(string, string) (string, error) { return "", fmt.Errorf("no keyring") },
	}
}

func tempRepo(t *testing.T, c *Client, url string, options TemporaryOptions) *Repo {
	t.Helper()
	repo, err := c.Temporary(context.Background(), url, options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}
