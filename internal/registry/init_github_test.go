package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/githubclient"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
)

type createCall struct {
	path    string
	name    string
	private bool
}

// fakeGitHub is an in-memory GitHub REST API. The login user is "me".
type fakeGitHub struct {
	mu              sync.Mutex
	repos           map[string]map[string]any
	creates         []createCall
	permissions     []map[string]any
	requests        int
	failCreate      int
	failPermissions int
	failGet         int
	// hideFromGet makes GET answer 404 for an existing repository once.
	hideFromGet bool
}

func repoJSON(owner, name string, private bool) map[string]any { //nolint:antislop // free-form JSON fixture for the fake API
	return map[string]any{
		"name": name, "private": private, "default_branch": "main",
		"owner":     map[string]any{"login": owner},
		"clone_url": "https://github.com/" + owner + "/" + name + ".git",
		"html_url":  "https://ghe.example.test/" + owner + "/" + name,
	}
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	reply := func(status int, body any) { //nolint:antislop // free-form JSON fixture for the fake API
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	failure := map[string]any{"message": "failed"}
	if r.Header.Get("Authorization") != "Bearer test-token" {
		reply(http.StatusUnauthorized, failure)
		return
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/user":
		reply(http.StatusOK, map[string]any{"login": "me"})
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/actions/permissions/workflow"):
		if f.failPermissions != 0 {
			reply(f.failPermissions, failure)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.permissions = append(f.permissions, body)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/repos/"):
		if f.failGet != 0 {
			reply(f.failGet, failure)
			return
		}
		repo, ok := f.repos[strings.ToLower(strings.TrimPrefix(path, "/repos/"))]
		if !ok || f.hideFromGet {
			f.hideFromGet = false
			reply(http.StatusNotFound, map[string]any{"message": "Not Found"})
			return
		}
		reply(http.StatusOK, repo)
	case r.Method == http.MethodPost && (path == "/user/repos" || strings.HasPrefix(path, "/orgs/")):
		if f.failCreate != 0 {
			reply(f.failCreate, failure)
			return
		}
		var body struct {
			Name    string `json:"name"`
			Private bool   `json:"private"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.creates = append(f.creates, createCall{path: path, name: body.Name, private: body.Private})
		owner := "me"
		if parts := strings.Split(path, "/"); parts[1] == "orgs" {
			owner = parts[2]
		}
		key := strings.ToLower(owner + "/" + body.Name)
		if _, exists := f.repos[key]; exists {
			reply(http.StatusUnprocessableEntity, map[string]any{"message": "name already exists on this account"})
			return
		}
		f.repos[key] = repoJSON(owner, body.Name, body.Private)
		reply(http.StatusCreated, f.repos[key])
	default:
		reply(http.StatusNotFound, map[string]any{"message": "unexpected " + r.Method + " " + path})
	}
}

func (f *fakeGitHub) snapshot() (creates []createCall, permissions []map[string]any, requests int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]createCall(nil), f.creates...), append([]map[string]any(nil), f.permissions...), f.requests
}

type publishHarness struct {
	t       *testing.T
	api     *fakeGitHub
	bare    string
	pushOut *atomic.Bool
	service Service
	catalog domain.Catalog
	target  string
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
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

func testCredentials(t *testing.T, token string) *gitclient.Credentials {
	t.Helper()
	home := t.TempDir()
	return &gitclient.Credentials{
		Getenv: func(name string) string {
			if name == "GITHUB_TOKEN" {
				return token
			}
			return ""
		},
		HomeDir: func() (string, error) { return home, nil },
		Keyring: func(string, string) (string, error) { return "", errors.New("no keyring") },
	}
}

// setIdentity isolates the global Git configuration Init reads.
func setIdentity(t *testing.T, configured bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if configured {
		data := "[user]\n\tname = Reg Istry\n\temail = reg@example.invalid\n"
		if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func newPublishHarness(t *testing.T) *publishHarness {
	t.Helper()
	setIdentity(t, true)
	h := &publishHarness{t: t, api: &fakeGitHub{repos: map[string]map[string]any{}}, bare: t.TempDir(), pushOut: &atomic.Bool{}}
	runGit(t, h.bare, "init", "-q", "--bare", "-b", "main")
	server := httptest.NewServer(h.api)
	t.Cleanup(server.Close)
	missing := "file://" + filepath.Join(t.TempDir(), "missing.git")
	credentials := testCredentials(t, "test-token")
	h.service = Service{
		UserGit: gitclient.New(gitclient.Options{
			Policy: gitclient.Policy{AllowFile: true, TrustedOrigin: true, Rewrite: func(string) string {
				if h.pushOut.Load() {
					return missing
				}
				return "file://" + h.bare
			}},
			Credentials: credentials,
			TempDir:     t.TempDir(),
		}),
		GitHub: githubclient.Options{Host: "github.com", BaseURL: server.URL, Credentials: credentials},
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	if h.catalog, err = h.service.Load(context.Background(), settings.Resolved{Settings: cfg}, false); err != nil {
		t.Fatal(err)
	}
	h.target = filepath.Join(t.TempDir(), "reg")
	return h
}

func (h *publishHarness) init(opts InitOptions) (InitResult, error) {
	h.t.Helper()
	opts.Path, opts.Version = h.target, "test"
	return h.service.Init(context.Background(), h.catalog, opts)
}

func (h *publishHarness) remoteHead() string {
	h.t.Helper()
	return runGit(h.t, h.bare, "rev-parse", "refs/heads/main")
}

func (h *publishHarness) localHead() string {
	h.t.Helper()
	return runGit(h.t, h.target, "rev-parse", "HEAD")
}

func (h *publishHarness) remoteEmpty() bool {
	h.t.Helper()
	return runGit(h.t, h.bare, "for-each-ref") == ""
}

func TestInitCreatesGitHubRepository(t *testing.T) {
	cases := []struct {
		name, repo, wantPath string
		public               bool
	}{
		{"personal private", "me/my-registry", "/user/repos", false},
		{"personal public", "me/my-registry", "/user/repos", true},
		{"organization private", "acme/my-registry", "/orgs/acme/repos", false},
		{"organization public", "acme/my-registry", "/orgs/acme/repos", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPublishHarness(t)
			if _, err := h.init(InitOptions{GitHubRepo: tc.repo, Public: tc.public, CI: true}); err != nil {
				t.Fatal(err)
			}
			creates, permissions, _ := h.api.snapshot()
			if len(creates) != 1 || creates[0] != (createCall{path: tc.wantPath, name: "my-registry", private: !tc.public}) {
				t.Fatalf("creates = %+v", creates)
			}
			if len(permissions) != 1 || permissions[0]["default_workflow_permissions"] != "read" || permissions[0]["can_approve_pull_request_reviews"] != true {
				t.Fatalf("permissions = %+v", permissions)
			}
			if h.localHead() != h.remoteHead() {
				t.Fatal("initial commit was not pushed")
			}
			if got := runGit(t, h.target, "log", "-1", "--format=%s|%an|%ae"); got != "Initial registry|Reg Istry|reg@example.invalid" {
				t.Fatal(got)
			}
			if got := runGit(t, h.target, "remote", "get-url", "origin"); got != "https://github.com/"+tc.repo+".git" {
				t.Fatal(got)
			}
			if got := runGit(t, h.target, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
				t.Fatal(got)
			}
			if got := runGit(t, h.target, "status", "--porcelain"); got != "" {
				t.Fatal("worktree not clean:", got)
			}
			if got := runGit(t, h.bare, "ls-tree", "-r", "--name-only", "refs/heads/main"); !strings.Contains(got, ".github/workflows/registry.yml") {
				t.Fatal("workflow not pushed:", got)
			}
		})
	}
}

func TestInitNamesPublishedRegistryAfterRepository(t *testing.T) {
	h := newPublishHarness(t)
	result, err := h.init(InitOptions{GitHubRepo: "Owner/my.registry"})
	if err != nil || result.RegistryID != "github:Owner:my.registry" {
		t.Fatal(result, err)
	}
	if result.PublishedURL != "https://ghe.example.test/Owner/my.registry" {
		t.Fatalf("the published URL must come from the API, for Enterprise hosts: %q", result.PublishedURL)
	}
	config, err := registrygen.Load(result.Path)
	if err != nil || config.Registry.ID != result.RegistryID {
		t.Fatal(config, err)
	}
}

func TestInitGitHubRepositoryValidationAndFailure(t *testing.T) {
	h := newPublishHarness(t)
	for _, repo := range []string{"no-slash", "-bad/name", "owner/", "owner/na me", "a/b/c"} {
		if _, err := h.init(InitOptions{GitHubRepo: repo}); err == nil {
			t.Fatalf("repo %q accepted", repo)
		}
	}
	if _, err := h.init(InitOptions{Public: true}); err == nil {
		t.Fatal("public without repository accepted")
	}
	if _, _, requests := h.api.snapshot(); requests != 0 {
		t.Fatal("validation reached GitHub")
	}
	h.api.failCreate = http.StatusForbidden
	result, err := h.init(InitOptions{GitHubRepo: "acme/repo"})
	var publish *PublishError
	if !errors.As(err, &publish) || publish.Stage != PublishRepository || !errors.Is(err, gitclient.KindPermission) {
		t.Fatalf("unexpected error %v", err)
	}
	if !strings.Contains(err.Error(), "registry created and committed locally; GitHub repository creation failed") {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("local registry should still be reported valid")
	}
	if h.localHead() == "" {
		t.Fatal("local commit missing")
	}
}

func TestInitLookupFailureNeverCreates(t *testing.T) {
	h := newPublishHarness(t)
	h.api.failGet = http.StatusInternalServerError
	_, err := h.init(InitOptions{GitHubRepo: "me/repo"})
	var publish *PublishError
	if !errors.As(err, &publish) || publish.Stage != PublishRepository {
		t.Fatal(err)
	}
	if creates, _, _ := h.api.snapshot(); len(creates) != 0 {
		t.Fatal("created after a failed lookup", creates)
	}
}

func TestInitMissingTokenKeepsLocalCommit(t *testing.T) {
	h := newPublishHarness(t)
	h.service.GitHub.Credentials = testCredentials(t, "")
	result, err := h.init(InitOptions{GitHubRepo: "me/repo"})
	var publish *PublishError
	if !errors.As(err, &publish) || publish.Stage != PublishAuth || !errors.Is(err, gitclient.KindAuth) {
		t.Fatal(err)
	}
	if !result.Valid || !strings.Contains(err.Error(), "GITHUB_TOKEN") || !strings.Contains(err.Error(), "committed locally") {
		t.Fatal(result, err)
	}
	if h.localHead() == "" {
		t.Fatal("local commit missing")
	}
	if _, _, requests := h.api.snapshot(); requests != 0 {
		t.Fatal("GitHub contacted without a token")
	}
}

func TestInitMissingIdentityIsActionable(t *testing.T) {
	h := newPublishHarness(t)
	setIdentity(t, false)
	result, err := h.init(InitOptions{GitHubRepo: "me/repo"})
	var publish *PublishError
	if !errors.As(err, &publish) || publish.Stage != PublishCommit || !errors.Is(err, gitclient.KindIdentityMissing) {
		t.Fatal(err)
	}
	if !result.Valid || !strings.Contains(err.Error(), "git config --global user.name") {
		t.Fatal(result, err)
	}
	if _, _, requests := h.api.snapshot(); requests != 0 {
		t.Fatal("GitHub contacted without a commit")
	}
}

func TestInitPushFailureRetryReusesRepository(t *testing.T) {
	h := newPublishHarness(t)
	h.pushOut.Store(true)
	result, err := h.init(InitOptions{GitHubRepo: "me/repo"})
	var publish *PublishError
	if !errors.As(err, &publish) || publish.Stage != PublishPush {
		t.Fatal(err)
	}
	if !result.Valid || !strings.Contains(err.Error(), "me/repo exists but the push failed") || !strings.Contains(err.Error(), "push -u origin main") {
		t.Fatal(result, err)
	}
	if !h.remoteEmpty() {
		t.Fatal("remote changed")
	}
	if got := runGit(t, h.target, "remote", "get-url", "origin"); got != "https://github.com/me/repo.git" {
		t.Fatal("origin not configured for a manual push:", got)
	}

	h.pushOut.Store(false)
	if err = os.RemoveAll(h.target); err != nil {
		t.Fatal(err)
	}
	if _, err = h.init(InitOptions{GitHubRepo: "me/repo"}); err != nil {
		t.Fatal(err)
	}
	if creates, _, _ := h.api.snapshot(); len(creates) != 1 {
		t.Fatal("retry created a duplicate", creates)
	}
	if h.localHead() != h.remoteHead() {
		t.Fatal("retry did not push")
	}
}

func TestInitRetryAdoptsIdenticalPublishedContent(t *testing.T) {
	h := newPublishHarness(t)
	if _, err := h.init(InitOptions{GitHubRepo: "me/repo", CI: true}); err != nil {
		t.Fatal(err)
	}
	first := h.remoteHead()
	if err := os.RemoveAll(h.target); err != nil {
		t.Fatal(err)
	}
	if _, err := h.init(InitOptions{GitHubRepo: "me/repo", CI: true}); err != nil {
		t.Fatal(err)
	}
	creates, permissions, _ := h.api.snapshot()
	if len(creates) != 1 || len(permissions) != 2 {
		t.Fatal(creates, permissions)
	}
	if h.remoteHead() != first || h.localHead() != first {
		t.Fatal("remote rewritten or local commit not adopted")
	}
	if got := runGit(t, h.target, "status", "--porcelain"); got != "" {
		t.Fatal("worktree not clean:", got)
	}
	if got := runGit(t, h.target, "rev-parse", "origin/main"); got != first {
		t.Fatal(got)
	}
}

func seedRemote(t *testing.T, h *publishHarness, branch string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "other")
	runGit(t, dir, "push", "-q", h.bare, branch)
	runGit(t, h.bare, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return runGit(t, dir, "rev-parse", "HEAD")
}

func TestInitRefusesConflictingRepository(t *testing.T) {
	cases := []struct {
		name string
		seed string
		repo map[string]any //nolint:antislop // free-form JSON fixture for the fake API
		want string
		opts InitOptions
	}{
		{name: "different content", seed: "main", repo: repoJSON("me", "repo", true), want: "different content"},
		{name: "other default branch", seed: "trunk", repo: repoJSON("me", "repo", true), want: `default branch is "trunk"`},
		{name: "public when private requested", repo: repoJSON("me", "repo", false), want: "it is public but a private"},
		{name: "private when public requested", repo: repoJSON("me", "repo", true), want: "it is private but a public", opts: InitOptions{Public: true}},
		{name: "renamed", repo: repoJSON("me", "renamed", true), want: "it is now me/renamed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPublishHarness(t)
			h.api.repos["me/repo"] = tc.repo
			before := ""
			if tc.seed != "" {
				before = seedRemote(t, h, tc.seed)
			}
			opts := tc.opts
			opts.GitHubRepo = "me/repo"
			result, err := h.init(opts)
			var publish *PublishError
			if !errors.As(err, &publish) || publish.Stage != PublishRepository || !errors.Is(err, ErrRepositoryConflict) {
				t.Fatal(err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "left unchanged") || !result.Valid {
				t.Fatal(result, err)
			}
			if creates, _, _ := h.api.snapshot(); len(creates) != 0 {
				t.Fatal("created", creates)
			}
			if tc.seed != "" && runGit(t, h.bare, "rev-parse", "refs/heads/"+tc.seed) != before {
				t.Fatal("remote overwritten")
			}
			if got := runGit(t, h.target, "remote"); got != "" {
				t.Fatal("origin set for a refused repository:", got)
			}
		})
	}
}

func TestInitCreateRaceReusesRepository(t *testing.T) {
	h := newPublishHarness(t)
	h.api.repos["me/repo"] = repoJSON("me", "repo", true)
	h.api.hideFromGet = true
	if _, err := h.init(InitOptions{GitHubRepo: "me/repo"}); err != nil {
		t.Fatal(err)
	}
	if h.localHead() != h.remoteHead() {
		t.Fatal("not pushed to the reused repository")
	}
}

// receivePackRejecting is a Git smart-HTTP endpoint whose push report rejects main.
func receivePackRejecting(reason string) http.Handler { //nolint:antislop // net/http/httptest.NewServer takes an http.Handler
	pkt := func(line string) string { return fmt.Sprintf("%04x%s", len(line)+4, line) }
	zero := strings.Repeat("0", 40)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
			_, _ = io.WriteString(w, pkt("# service=git-receive-pack\n")+"0000"+pkt(zero+" capabilities^{}\x00report-status\n")+"0000")
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		_, _ = io.WriteString(w, pkt("unpack ok\n")+pkt("ng refs/heads/main "+reason+"\n")+"0000")
	})
}

func TestInitDiagnosesMissingWorkflowScope(t *testing.T) {
	h := newPublishHarness(t)
	server := httptest.NewServer(receivePackRejecting("refusing to allow a Personal Access Token to create or update workflow `.github/workflows/registry.yml` without `workflow` scope"))
	t.Cleanup(server.Close)
	h.service.UserGit = gitclient.New(gitclient.Options{
		Policy:      gitclient.Policy{AllowHTTP: true, TrustedOrigin: true, Rewrite: func(string) string { return server.URL + "/me/repo.git" }},
		Credentials: testCredentials(t, "test-token"),
		TempDir:     t.TempDir(),
	})
	_, err := h.init(InitOptions{GitHubRepo: "me/repo", CI: true})
	var publish *PublishError
	if !errors.As(err, &publish) || publish.Stage != PublishPush {
		t.Fatal(err)
	}
	if !strings.Contains(err.Error(), "lacks the `workflow` scope") || !strings.Contains(err.Error(), "gh auth refresh -s workflow") {
		t.Fatal(err)
	}
}

func TestPushAdvice(t *testing.T) {
	permission := &gitclient.Error{Kind: gitclient.KindPermission, Op: "push"}
	if got := pushAdvice(permission, "me/repo"); !strings.Contains(got, "write access") {
		t.Fatal(got)
	}
	if got := pushAdvice(errors.New("network down"), "me/repo"); !strings.Contains(got, "connection") {
		t.Fatal(got)
	}
}

func TestInitGitUsesMainBranch(t *testing.T) {
	setIdentity(t, false)
	result, err := initFixture(t, InitOptions{Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, result.Path, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
		t.Fatal(got)
	}
	if got := runGit(t, result.Path, "for-each-ref"); got != "" {
		t.Fatal("--git must not commit:", got)
	}
}

func TestInitWarnsWhenActionsPermissionFails(t *testing.T) {
	h := newPublishHarness(t)
	h.api.failPermissions = http.StatusForbidden
	result, err := h.init(InitOptions{GitHubRepo: "me/repo", CI: true})
	if err != nil {
		t.Fatal("the repository already exists, so this must not fail:", err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "W_ACTIONS_PULL_REQUESTS" || result.Diagnostics[0].Severity != "warning" || result.Diagnostics[0].Subject != "me/repo" {
		t.Fatal(result.Diagnostics)
	}
	if h.localHead() != h.remoteHead() {
		t.Fatal("push missing")
	}
}

func TestInitWithoutCISkipsActionsPermission(t *testing.T) {
	h := newPublishHarness(t)
	if _, err := h.init(InitOptions{GitHubRepo: "me/repo"}); err != nil {
		t.Fatal(err)
	}
	if _, permissions, _ := h.api.snapshot(); len(permissions) != 0 {
		t.Fatal("permission changed without CI", permissions)
	}
}

func TestPublishedURLFallsBackToTheAPIHost(t *testing.T) {
	for _, tc := range []struct{ reported, host, want string }{
		{"https://ghe.example.test/o/r", "ghe.example.test", "https://ghe.example.test/o/r"},
		{"", "ghe.example.test", "https://ghe.example.test/o/r"},
		{"", "", "https://github.com/o/r"},
	} {
		if got := publishedURL(tc.reported, tc.host, "o/r"); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
}
