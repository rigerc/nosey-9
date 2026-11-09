package registrygen

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

const upstreamURL = "https://github.com/owner/upstream"

func upstreamRepo(t *testing.T) (dir, first, second string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		// #nosec G204 -- test helper with a fixed binary and test-controlled arguments.
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
	commit := func(content string) string {
		writeFile(t, dir, "data/skills.json", content)
		git("add", "-A")
		git("commit", "-q", "-m", content)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	git("config", "uploadpack.allowAnySHA1InWant", "true")
	first = commit("one")
	git("tag", "v1")
	return dir, first, commit("two")
}

func nativeFetcher(t *testing.T, repo string) GitFetcher {
	t.Helper()
	missing := "file://" + filepath.Join(t.TempDir(), "missing.git")
	rewrite := func(url string) string {
		if url == upstreamURL {
			return "file://" + repo
		}
		return missing
	}
	home := t.TempDir()
	return GitFetcher{Client: gitclient.New(gitclient.Options{
		Policy: gitclient.Policy{AllowFile: true, Rewrite: rewrite},
		Credentials: &gitclient.Credentials{
			Getenv:  func(string) string { return "" },
			HomeDir: func() (string, error) { return home, nil },
			Keyring: func(string, string) (string, error) { return "", errors.New("no keyring") },
		},
		TempDir: t.TempDir(),
	})}
}

func TestGitFetcherResolvesRevisionAndReadsDeclaredFile(t *testing.T) {
	repo, first, second := upstreamRepo(t)
	fetcher := nativeFetcher(t, repo)
	for ref, want := range map[string]struct{ revision, data string }{
		"":     {second, "two"},
		"main": {second, "two"},
		"v1":   {first, "one"},
		first:  {first, "one"},
	} {
		revision, data, err := fetcher.Fetch(context.Background(), Upstream{URL: upstreamURL, Ref: ref, Path: "data/skills.json"})
		if err != nil || revision != want.revision || string(data) != want.data {
			t.Fatalf("ref %q: %s %q %v", ref, revision, data, err)
		}
	}
}

func TestGitFetcherFailures(t *testing.T) {
	repo, _, _ := upstreamRepo(t)
	fetcher := nativeFetcher(t, repo)
	for name, c := range map[string]struct {
		upstream Upstream
		want     gitclient.Kind
	}{
		"path": {Upstream{URL: upstreamURL, Path: "absent.json"}, gitclient.KindMissingPath},
		"ref":  {Upstream{URL: upstreamURL, Ref: "absent", Path: "data/skills.json"}, gitclient.KindMissingRef},
		"repo": {Upstream{URL: "https://github.com/owner/other", Path: "data/skills.json"}, gitclient.KindMissingRepository},
	} {
		if _, _, err := fetcher.Fetch(context.Background(), c.upstream); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := fetcher.Fetch(context.Background(), Upstream{URL: "file:///tmp/repo", Path: "x"}); err == nil {
		t.Fatal("unsafe upstream accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := fetcher.Fetch(canceled, Upstream{URL: upstreamURL, Path: "data/skills.json"}); !errors.Is(err, gitclient.KindCanceled) {
		t.Fatal(err)
	}
}
