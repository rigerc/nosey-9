package gitclient

import (
	"context"
	"net/http"
	"net/http/cgi" //nolint:gosec // test fixture serves git-http-backend; Httpoxy does not apply
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func openUserClient(t *testing.T) *Client {
	t.Helper()
	return New(Options{Policy: Policy{AllowFile: true}, Credentials: emptyCredentials(t.TempDir()), TempDir: t.TempDir()})
}

func committedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	gitCmd(t, dir, "config", "user.name", "a")
	gitCmd(t, dir, "config", "user.email", "a@a")
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	writeFile(t, filepath.Join(dir, "kept/a.txt"), "x")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", "1")
	return dir
}

func TestOpenUserToleratesBenignExtensions(t *testing.T) {
	for _, extension := range []string{"partialClone=origin", "worktreeConfig=true", "preciousObjects=true"} {
		t.Run(extension, func(t *testing.T) {
			dir := committedRepo(t)
			name, value, _ := strings.Cut(extension, "=")
			gitCmd(t, dir, "config", "core.repositoryformatversion", "1")
			gitCmd(t, dir, "config", "extensions."+name, value)
			repo, err := openUserClient(t).OpenUser(filepath.Join(dir, "kept"))
			require.NoError(t, err)
			head, err := repo.Head()
			require.NoError(t, err)
			require.Equal(t, gitCmd(t, dir, "rev-parse", "HEAD"), head)
			require.Equal(t, resolved(t, dir), resolved(t, repo.Dir()))
		})
	}
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return canonical
}

func TestOpenUserKeepsExtensionsOnConfigWrite(t *testing.T) {
	dir := committedRepo(t)
	gitCmd(t, dir, "config", "core.repositoryformatversion", "1")
	gitCmd(t, dir, "config", "extensions.worktreeConfig", "true")
	repo, err := openUserClient(t).OpenUser(dir)
	require.NoError(t, err)
	require.NoError(t, repo.SetOrigin("/some/path/origin.git"))
	require.Equal(t, "true", gitCmd(t, dir, "config", "extensions.worktreeConfig"))
}

func TestOpenUserRejectsUnreadableFormats(t *testing.T) {
	for name, kind := range map[string]Kind{"objectformat": KindUnsupportedFormat, "refstorage": KindUnsupportedFormat, "somethingnew": KindFailed} {
		t.Run(name, func(t *testing.T) {
			dir := committedRepo(t)
			gitCmd(t, dir, "config", "core.repositoryformatversion", "1")
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".git/config"), append(mustRead(t, filepath.Join(dir, ".git/config")), []byte("[extensions]\n\t"+name+" = sha256\n")...), 0o644)) //nolint:gosec // test fixture under t.TempDir
			_, err := openUserClient(t).OpenUser(dir)
			require.Error(t, err)
			require.Equal(t, kind, KindOf(err))
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test fixture under t.TempDir
	require.NoError(t, err)
	return data
}

func TestOpenUserFollowsGitFileAndCommonDir(t *testing.T) {
	dir := committedRepo(t)
	worktree := filepath.Join(t.TempDir(), "linked")
	gitCmd(t, dir, "worktree", "add", "-q", "-b", "other", worktree)
	repo, err := openUserClient(t).OpenUser(worktree)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	require.Equal(t, gitCmd(t, worktree, "rev-parse", "HEAD"), head)
	branch, err := repo.Branch()
	require.NoError(t, err)
	require.Equal(t, "other", branch)
	require.Equal(t, resolved(t, worktree), resolved(t, repo.Dir()))
}

func TestOpenUserOutsideRepository(t *testing.T) {
	_, err := openUserClient(t).OpenUser(t.TempDir())
	require.Equal(t, KindMissingRepository, KindOf(err))
}

func TestOpenUserRealSparseCheckout(t *testing.T) {
	dir := committedRepo(t)
	writeFile(t, filepath.Join(dir, "gone/b.txt"), "y")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", "2")
	gitCmd(t, dir, "sparse-checkout", "set", "kept")
	require.Equal(t, "true", gitCmd(t, dir, "config", "--get", "extensions.worktreeConfig"))
	writeFile(t, filepath.Join(dir, "ignored.txt"), "z")
	writeFile(t, filepath.Join(dir, "new.txt"), "z")
	repo, err := openUserClient(t).OpenUser(dir)
	require.NoError(t, err)
	listed, err := repo.ListWorktreeFiles(context.Background(), dir)
	require.NoError(t, err)
	want := strings.Split(gitCmd(t, dir, "ls-files", "--cached", "--others", "--exclude-standard", "--", "."), "\n")
	sort.Strings(want)
	require.Equal(t, want, listed)
	require.NotContains(t, listed, "ignored.txt")
}

func TestOpenUserRealPartialClone(t *testing.T) {
	f := newFixture(t, "uploadpack.allowFilter=true", "uploadpack.allowAnySHA1InWant=true")
	clone := filepath.Join(t.TempDir(), "clone")
	gitCmd(t, t.TempDir(), "clone", "-q", "--filter=blob:none", "--no-checkout", f.url(), clone)
	require.Equal(t, "true", gitCmd(t, clone, "config", "--get", "remote.origin.promisor"))
	gitCmd(t, clone, "config", "core.repositoryformatversion", "1")
	gitCmd(t, clone, "config", "extensions.partialClone", "origin")
	c := openUserClient(t)
	repo, err := c.OpenUser(clone)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	err = repo.Materialize(context.Background(), head, filepath.Join(t.TempDir(), "out"))
	require.Equal(t, KindMissingBlob, KindOf(err), "%v", err)
	dest := filepath.Join(t.TempDir(), "exported")
	require.NoError(t, repo.ExportOrigin(context.Background(), head, dest))
	require.Equal(t, "v3\n", string(mustRead(t, filepath.Join(dest, "skills/a/SKILL.md"))))
}

// credentialServer serves bare over smart HTTP and records Authorization headers.
type credentialServer struct {
	*httptest.Server
	mu      sync.Mutex
	headers []string
	reject  func(*http.Request) int
}

func newCredentialServer(t *testing.T, bare string) *credentialServer {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	require.NoError(t, err)
	backend := &cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(bare), "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=tester"},
	}
	server := &credentialServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		server.headers = append(server.headers, r.Header.Get("Authorization"))
		server.mu.Unlock()
		if server.reject != nil {
			if status := server.reject(r); status != 0 {
				w.WriteHeader(status)
				return
			}
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *credentialServer) url(bare string) string {
	return s.URL + "/" + filepath.Base(bare)
}

func enterpriseCredentials(t *testing.T, host, token string) *Credentials {
	t.Helper()
	return &Credentials{
		Getenv: func(name string) string {
			if name == "GH_ENTERPRISE_TOKEN" {
				return token
			}
			return ""
		},
		TrustedHosts: []string{host},
		HomeDir:      func() (string, error) { return t.TempDir(), nil },
		Keyring:      func(string, string) (string, error) { return "", os.ErrNotExist },
	}
}

func TestEmbeddedCredentialsAreUsedAndNeverExposed(t *testing.T) {
	f := newFixture(t)
	server := newCredentialServer(t, f.bare)
	const secret = "s3cr3t-embedded"
	remote := strings.Replace(server.url(f.bare), "http://", "http://user:"+secret+"@", 1)
	dir := committedRepo(t)
	gitCmd(t, dir, "remote", "add", "origin", remote)
	c := New(Options{Policy: UserOrigin(), Credentials: emptyCredentials(t.TempDir()), TempDir: t.TempDir()})
	repo, err := c.OpenUser(dir)
	require.NoError(t, err)
	origin, err := repo.OriginURL()
	require.NoError(t, err)
	require.NotContains(t, origin, secret)
	snapshot, commit, err := repo.FetchOriginSnapshot(context.Background(), "refs/heads/main")
	require.NoError(t, err)
	defer func() { _ = snapshot.Close() }()
	require.Equal(t, f.commits[2], commit)
	server.mu.Lock()
	require.NotEmpty(t, server.headers)
	sent := server.headers[0]
	server.mu.Unlock()
	require.True(t, strings.HasPrefix(sent, "Basic "), sent)
	require.NotContains(t, string(mustRead(t, filepath.Join(snapshot.Dir(), "config"))), secret)

	_, err = c.ResolveRemote(context.Background(), remote+"-missing", "main")
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
	_, err = c.ResolveRemote(context.Background(), "http://user:"+secret+"@ho%zzst/x", "main")
	require.Error(t, err)
	require.Equal(t, KindInvalidSource, KindOf(err))
	require.NotContains(t, err.Error(), secret)
	_, err = New(Options{Policy: Hardened()}).ResolveRemote(context.Background(), "https://user:"+secret+"@github.com/o/r", "main")
	require.Equal(t, KindInvalidSource, KindOf(err))
}

func TestStaleTokenFallsBackToAnonymousRead(t *testing.T) {
	f := newFixture(t)
	server := newCredentialServer(t, f.bare)
	server.reject = func(r *http.Request) int {
		if r.Header.Get("Authorization") != "" {
			return http.StatusUnauthorized
		}
		return 0
	}
	host := strings.TrimPrefix(server.URL, "http://")
	c := New(Options{Policy: UserOrigin(), Credentials: enterpriseCredentials(t, strings.Split(host, ":")[0], "stale"), TempDir: t.TempDir()})
	found, err := c.ResolveRemote(context.Background(), server.url(f.bare), "main")
	require.NoError(t, err)
	require.Equal(t, f.commits[2], found.Commit)
	repo := tempRepo(t, c, server.url(f.bare), TemporaryOptions{})
	commit, err := repo.FetchRef(context.Background(), "main")
	require.NoError(t, err)
	require.Equal(t, f.commits[2], commit)
}

func TestPrivateRepositoryStaysAuthFailure(t *testing.T) {
	f := newFixture(t)
	for name, anonymous := range map[string]int{"unauthorized": http.StatusUnauthorized, "not found": http.StatusNotFound} {
		t.Run(name, func(t *testing.T) {
			server := newCredentialServer(t, f.bare)
			server.reject = func(r *http.Request) int {
				if r.Header.Get("Authorization") != "" {
					return http.StatusUnauthorized
				}
				return anonymous
			}
			host := strings.TrimPrefix(server.URL, "http://")
			c := New(Options{Policy: UserOrigin(), Credentials: enterpriseCredentials(t, strings.Split(host, ":")[0], "stale"), TempDir: t.TempDir()})
			_, err := c.ResolveRemote(context.Background(), server.url(f.bare), "main")
			require.Equal(t, KindAuth, KindOf(err), "%v", err)
			repo := tempRepo(t, c, server.url(f.bare), TemporaryOptions{})
			_, err = repo.FetchRef(context.Background(), "main")
			require.Equal(t, KindAuth, KindOf(err), "%v", err)
		})
	}
}

func TestPushDoesNotRetryAnonymously(t *testing.T) {
	f := newFixture(t)
	server := newCredentialServer(t, f.bare)
	gitCmd(t, f.bare, "config", "http.receivepack", "true")
	server.reject = func(r *http.Request) int {
		if strings.Contains(r.URL.RawQuery, "receive-pack") || strings.Contains(r.URL.Path, "receive-pack") {
			if r.Header.Get("Authorization") != "" {
				return http.StatusUnauthorized
			}
			t.Errorf("anonymous push attempt: %s", r.URL)
		}
		return 0
	}
	host := strings.TrimPrefix(server.URL, "http://")
	c := New(Options{Policy: UserOrigin(), Credentials: enterpriseCredentials(t, strings.Split(host, ":")[0], "stale"), TempDir: t.TempDir()})
	repo := tempRepo(t, c, server.url(f.bare), TemporaryOptions{Worktree: true})
	commit, err := repo.FetchRef(context.Background(), "main")
	require.NoError(t, err)
	require.NoError(t, repo.PrepareWorktree(context.Background(), commit))
	writeFile(t, filepath.Join(repo.Dir(), "new"), "x")
	committed, err := repo.Commit(context.Background(), CommitOptions{Message: "m", Identity: Identity{Name: "a", Email: "a@a"}})
	require.NoError(t, err)
	err = repo.Push(context.Background(), committed, "feature")
	require.Equal(t, KindAuth, KindOf(err), "%v", err)
}

func TestCommitNormalizesLikeGit(t *testing.T) {
	attributes := "*.crlf text eol=crlf\n*.auto text=auto\n*.bin -text\n"
	files := map[string]string{"a.crlf": "one\r\ntwo\r\n", "b.auto": "x\r\n", "c.bin": "raw\r\n", "d.txt": "plain\r\n", ".gitattributes": attributes}
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", "cli")
	want := gitCmd(t, dir, "rev-parse", "HEAD^{tree}")

	native := t.TempDir()
	gitCmd(t, native, "init", "-q", "-b", "main")
	gitCmd(t, native, "config", "user.name", "a")
	gitCmd(t, native, "config", "user.email", "a@a")
	for name, content := range files {
		writeFile(t, filepath.Join(native, name), content)
	}
	repo, err := New(Options{Credentials: emptyCredentials(t.TempDir())}).OpenUser(native)
	require.NoError(t, err)
	_, err = repo.Commit(context.Background(), CommitOptions{Message: "native"})
	require.NoError(t, err)
	require.Equal(t, want, gitCmd(t, native, "rev-parse", "HEAD^{tree}"))
	require.Equal(t, files["a.crlf"], string(mustRead(t, filepath.Join(native, "a.crlf"))), "the working file is left as written")
}

func TestDirtyMatchesGitStatus(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"clean":     func(*testing.T, string) {},
		"modified":  func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "kept/a.txt"), "changed") },
		"deleted":   func(t *testing.T, dir string) { require.NoError(t, os.Remove(filepath.Join(dir, "kept/a.txt"))) },
		"untracked": func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "new.txt"), "n") },
		"ignored":   func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "ignored.txt"), "n") },
		"staged": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "kept/a.txt"), "changed")
			gitCmd(t, dir, "add", "-A")
		},
		"stat only": func(t *testing.T, dir string) { touch(t, filepath.Join(dir, "kept/a.txt")) },
		"nested repository": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "sub/f"), "x")
			gitCmd(t, filepath.Join(dir, "sub"), "init", "-q")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := committedRepo(t)
			mutate(t, dir)
			assertDirtyParity(t, dir)
		})
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(path, info.ModTime().Add(3600e9), info.ModTime().Add(3600e9)))
}

func assertDirtyParity(t *testing.T, dir string) {
	t.Helper()
	repo, err := New(Options{Credentials: emptyCredentials(t.TempDir())}).OpenUser(dir)
	require.NoError(t, err)
	dirty, err := repo.Dirty(context.Background())
	require.NoError(t, err)
	status := gitCmd(t, dir, "status", "--porcelain")
	require.Equal(t, status != "", dirty, "git status: %q", status)
}

func TestDirtyIgnoresCRLFAndHonoursAutocrlf(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, ".gitattributes"), "*.txt text eol=crlf\n")
	writeFile(t, filepath.Join(dir, "a.txt"), "line\r\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", "1")
	touch(t, filepath.Join(dir, "a.txt"))
	assertDirtyParity(t, dir)

	auto := t.TempDir()
	gitCmd(t, auto, "init", "-q", "-b", "main")
	gitCmd(t, auto, "config", "core.autocrlf", "true")
	writeFile(t, filepath.Join(auto, "b.txt"), "line\r\n")
	gitCmd(t, auto, "add", "-A")
	gitCmd(t, auto, "commit", "-q", "-m", "1")
	touch(t, filepath.Join(auto, "b.txt"))
	assertDirtyParity(t, auto)
}

func TestAheadBehindMatchesRevList(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.src, "local"), "x")
	gitCmd(t, f.src, "add", "-A")
	gitCmd(t, f.src, "commit", "-q", "-m", "local")
	remoteSide := t.TempDir()
	gitCmd(t, remoteSide, "clone", "-q", f.bare, "up")
	up := filepath.Join(remoteSide, "up")
	writeFile(t, filepath.Join(up, "remote1"), "x")
	gitCmd(t, up, "add", "-A")
	gitCmd(t, up, "commit", "-q", "-m", "r1")
	writeFile(t, filepath.Join(up, "remote2"), "x")
	gitCmd(t, up, "add", "-A")
	gitCmd(t, up, "commit", "-q", "-m", "r2")
	gitCmd(t, up, "push", "-q", "origin", "main")
	gitCmd(t, f.src, "remote", "add", "origin", f.url())
	before, err := os.ReadDir(filepath.Join(f.src, ".git", "objects"))
	require.NoError(t, err)

	repo, err := openUserClient(t).OpenUser(f.src)
	require.NoError(t, err)
	snapshot, commit, err := repo.FetchOriginSnapshot(context.Background(), "refs/heads/main")
	require.NoError(t, err)
	defer func() { _ = snapshot.Close() }()
	head := gitCmd(t, f.src, "rev-parse", "HEAD")
	ahead, behind, err := snapshot.AheadBehind(context.Background(), head, commit)
	require.NoError(t, err)
	require.Equal(t, 1, ahead)
	require.Equal(t, 2, behind)
	require.NoError(t, snapshot.Materialize(context.Background(), commit, filepath.Join(t.TempDir(), "view")))
	after, err := os.ReadDir(filepath.Join(f.src, ".git", "objects"))
	require.NoError(t, err)
	require.Equal(t, before, after, "the user's object store must be untouched")
	require.Equal(t, head, gitCmd(t, f.src, "rev-parse", "HEAD"))
}

func TestSnapshotOfShallowRepositoryDoesNotLink(t *testing.T) {
	f := newFixture(t, "uploadpack.allowAnySHA1InWant=true")
	clone := filepath.Join(t.TempDir(), "clone")
	gitCmd(t, t.TempDir(), "clone", "-q", "--depth=1", f.url(), clone)
	repo, err := openUserClient(t).OpenUser(clone)
	require.NoError(t, err)
	snapshot, commit, err := repo.FetchOriginSnapshot(context.Background(), "refs/heads/main")
	require.NoError(t, err)
	defer func() { _ = snapshot.Close() }()
	require.Equal(t, f.commits[2], commit)
	require.NoFileExists(t, filepath.Join(snapshot.Dir(), "objects/info/alternates"))
}

func TestOriginLocation(t *testing.T) {
	cases := map[string]OriginLocation{
		"https://github.com/o/r.git":      {"github.com", "o", "r"},
		"git@github.com:o/r.git":          {"github.com", "o", "r"},
		"ssh://git@ghe.corp:2222/o/r":     {"ghe.corp", "o", "r"},
		"https://ghe.corp:8443/o/r":       {"ghe.corp:8443", "o", "r"},
		"https://user:tok@github.com/o/r": {"github.com", "o", "r"},
	}
	for origin, want := range cases {
		dir := committedRepo(t)
		gitCmd(t, dir, "remote", "add", "origin", origin)
		repo, err := New(Options{Policy: UserOrigin(), Credentials: emptyCredentials(t.TempDir())}).OpenUser(dir)
		require.NoError(t, err)
		got, ok, err := repo.OriginLocation()
		require.NoError(t, err, origin)
		require.True(t, ok, origin)
		require.Equal(t, want, got, origin)
	}
	dir := committedRepo(t)
	gitCmd(t, dir, "remote", "add", "origin", "https://gitlab.com/a/b/c")
	repo, err := New(Options{Policy: UserOrigin(), Credentials: emptyCredentials(t.TempDir())}).OpenUser(dir)
	require.NoError(t, err)
	_, ok, err := repo.OriginLocation()
	require.NoError(t, err)
	require.False(t, ok)
}
