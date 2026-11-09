package githubclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

var ctx = context.Background()

func credentials(t *testing.T, env map[string]string, trusted ...string) *gitclient.Credentials {
	t.Helper()
	home := t.TempDir()
	return &gitclient.Credentials{
		TrustedHosts: trusted,
		Getenv:       func(name string) string { return env[name] },
		HomeDir:      func() (string, error) { return home, nil },
		Keyring:      func(string, string) (string, error) { return "", fmt.Errorf("no keyring") },
	}
}

type recorded struct {
	method, path, query, auth string
	body                      map[string]any //nolint:antislop // free-form JSON fixture for the fake API
}

// serve answers each "METHOD /path" with status and body, recording requests.
func serve(t *testing.T, routes map[string]func(http.ResponseWriter)) (*Client, *[]recorded) {
	t.Helper()
	calls := &[]recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := recorded{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, auth: r.Header.Get("Authorization")}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			_ = json.Unmarshal(data, &call.body)
		}
		*calls = append(*calls, call)
		route, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		route(w)
	}))
	t.Cleanup(server.Close)
	client, err := New(ctx, Options{Credentials: credentials(t, map[string]string{"GITHUB_TOKEN": "tok"}), BaseURL: server.URL})
	require.NoError(t, err)
	return client, calls
}

func reply(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

const repoJSON = `{"name":"r","private":true,"default_branch":"main","html_url":"https://h/o/r","clone_url":"https://h/o/r.git","ssh_url":"git@h:o/r.git","owner":{"login":"o"}}`

func TestGetRepositorySendsTokenAndMapsFields(t *testing.T) {
	client, calls := serve(t, map[string]func(http.ResponseWriter){"GET /repos/o/r": reply(200, repoJSON)})
	repo, err := client.GetRepository(ctx, "o", "r")
	require.NoError(t, err)
	require.Equal(t, Repository{Owner: "o", Name: "r", Private: true, DefaultBranch: "main", HTMLURL: "https://h/o/r", CloneURL: "https://h/o/r.git", SSHURL: "git@h:o/r.git"}, repo)
	require.Equal(t, "Bearer tok", (*calls)[0].auth)
	require.True(t, client.Authenticated())
}

func TestAnonymousReadAndRequiredAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, repoJSON)
	}))
	defer server.Close()
	client, err := New(ctx, Options{Credentials: credentials(t, nil), BaseURL: server.URL})
	require.NoError(t, err)
	require.False(t, client.Authenticated())
	_, err = client.GetRepository(ctx, "o", "r")
	require.NoError(t, err)

	_, err = New(ctx, Options{Credentials: credentials(t, nil), RequireAuth: true})
	require.ErrorIs(t, err, gitclient.KindAuth)
}

func TestCreateRepositoryPersonalVersusOrganization(t *testing.T) {
	routes := map[string]func(http.ResponseWriter){
		"GET /user":             reply(200, `{"login":"Alice"}`),
		"POST /user/repos":      reply(201, repoJSON),
		"POST /orgs/acme/repos": reply(201, repoJSON),
	}
	client, calls := serve(t, routes)

	_, err := client.CreateRepository(ctx, CreateRepositoryRequest{Owner: "alice", Name: "r", Private: true})
	require.NoError(t, err, "owner comparison is case-insensitive")
	require.Equal(t, "/user/repos", (*calls)[1].path)
	require.Equal(t, "r", (*calls)[1].body["name"])
	require.Equal(t, true, (*calls)[1].body["private"])

	*calls = nil
	_, err = client.CreateRepository(ctx, CreateRepositoryRequest{Owner: "acme", Name: "r"})
	require.NoError(t, err)
	require.Equal(t, "/orgs/acme/repos", (*calls)[1].path)
	require.Equal(t, false, (*calls)[1].body["private"])

	*calls = nil
	_, err = client.CreateRepository(ctx, CreateRepositoryRequest{Name: "r"})
	require.NoError(t, err)
	require.Len(t, *calls, 1, "no owner needs no user lookup")
	require.Equal(t, "/user/repos", (*calls)[0].path)
}

func TestCreateRepositoryUserLookupFailure(t *testing.T) {
	client, _ := serve(t, map[string]func(http.ResponseWriter){"GET /user": reply(401, `{"message":"Bad credentials"}`)})
	_, err := client.CreateRepository(ctx, CreateRepositoryRequest{Owner: "a", Name: "r"})
	require.ErrorIs(t, err, gitclient.KindAuth)
}

func TestUpdateDefaultWorkflowPermissions(t *testing.T) {
	client, calls := serve(t, map[string]func(http.ResponseWriter){"PUT /repos/o/r/actions/permissions/workflow": reply(204, "")})
	require.NoError(t, client.UpdateDefaultWorkflowPermissions(ctx, "o", "r", WorkflowPermissions{Default: "read", CanApprovePullRequests: true}))
	require.Equal(t, map[string]any{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": true}, (*calls)[0].body)
}

const pullJSON = `{"number":7,"html_url":"https://h/o/r/pull/7","title":"T","draft":true,"head":{"ref":"studio/x"},"base":{"ref":"main"}}`

func TestCreatePullRequest(t *testing.T) {
	client, calls := serve(t, map[string]func(http.ResponseWriter){"POST /repos/o/r/pulls": reply(201, pullJSON)})
	pull, err := client.CreatePullRequest(ctx, CreatePullRequestRequest{Owner: "o", Repo: "r", Base: "main", Head: "studio/x", Title: "T", Body: "B"})
	require.NoError(t, err)
	require.Equal(t, PullRequest{Number: 7, URL: "https://h/o/r/pull/7", Head: "studio/x", Base: "main", Title: "T", IsDraft: true}, pull)
	require.Equal(t, map[string]any{"title": "T", "head": "studio/x", "base": "main", "body": "B"}, (*calls)[0].body)
}

func TestFindPullRequest(t *testing.T) {
	client, calls := serve(t, map[string]func(http.ResponseWriter){"GET /repos/o/r/pulls": reply(200, "["+pullJSON+"]")})
	pull, found, err := client.FindPullRequest(ctx, "o", "r", "studio/x", "main")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 7, pull.Number)
	require.Equal(t, "base=main&head=o%3Astudio%2Fx&state=open", (*calls)[0].query)

	client, _ = serve(t, map[string]func(http.ResponseWriter){"GET /repos/o/r/pulls": reply(200, "[]")})
	_, found, err = client.FindPullRequest(ctx, "o", "r", "x", "main")
	require.NoError(t, err)
	require.False(t, found)
}

func TestErrorStatusesAreTyped(t *testing.T) {
	cases := map[int]gitclient.Kind{
		401: gitclient.KindAuth,
		403: gitclient.KindPermission,
		404: gitclient.KindMissingRepository,
		422: KindInvalid,
		500: gitclient.KindFailed,
	}
	for status, kind := range cases {
		client, _ := serve(t, map[string]func(http.ResponseWriter){"GET /repos/o/r": reply(status, `{"message":"nope"}`)})
		_, err := client.GetRepository(ctx, "o", "r")
		require.ErrorIs(t, err, kind, "status %d", status)
		var typed *Error
		require.ErrorAs(t, err, &typed)
		require.Equal(t, status, typed.Status)
	}
}

func TestRateLimits(t *testing.T) {
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	client, _ := serve(t, map[string]func(http.ResponseWriter){"GET /repos/o/r": func(w http.ResponseWriter) {
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
	}})
	_, err := client.GetRepository(ctx, "o", "r")
	require.ErrorIs(t, err, KindRateLimited)
	var typed *Error
	require.ErrorAs(t, err, &typed)
	require.True(t, typed.RateReset.Equal(reset))

	client, _ = serve(t, map[string]func(http.ResponseWriter){"GET /repos/o/r": func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"You have exceeded a secondary rate limit","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`)
	}})
	_, err = client.GetRepository(ctx, "o", "r")
	require.ErrorIs(t, err, KindRateLimited)
	require.ErrorAs(t, err, &typed)
	require.False(t, typed.RateReset.IsZero())
}

func TestCancellationAndTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	client, err := New(ctx, Options{Credentials: credentials(t, nil), BaseURL: server.URL, Timeout: 20 * time.Millisecond})
	require.NoError(t, err)
	started := time.Now()
	_, err = client.GetRepository(ctx, "o", "r")
	require.Error(t, err, "the API timeout bounds a hung request")
	require.Less(t, time.Since(started), 5*time.Second)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = client.GetRepository(canceled, "o", "r")
	require.ErrorIs(t, err, gitclient.KindCanceled)
}

func TestHostAwareEndpointsAndTokenScope(t *testing.T) {
	env := map[string]string{"GITHUB_TOKEN": "com", "GH_ENTERPRISE_TOKEN": "ent"}

	client, err := New(ctx, Options{Credentials: credentials(t, env)})
	require.NoError(t, err)
	require.Equal(t, "https://api.github.com/", client.api.BaseURL())
	require.True(t, client.Authenticated())

	client, err = New(ctx, Options{Host: "api.github.com", Credentials: credentials(t, env)})
	require.NoError(t, err)
	require.Equal(t, "https://api.github.com/", client.api.BaseURL())

	client, err = New(ctx, Options{Host: "ghe.example.test", Credentials: credentials(t, env, "ghe.example.test")})
	require.NoError(t, err)
	require.Equal(t, "https://ghe.example.test/api/v3/", client.api.BaseURL())
	require.True(t, client.Authenticated())

	client, err = New(ctx, Options{Host: "tenant.ghe.com", Credentials: credentials(t, env, "tenant.ghe.com")})
	require.NoError(t, err)
	require.Contains(t, client.api.BaseURL(), "https://api.tenant.ghe.com/")

	client, err = New(ctx, Options{Host: "unknown.example.test", Credentials: credentials(t, env)})
	require.NoError(t, err)
	require.False(t, client.Authenticated(), "an unknown host receives no token")

	_, err = New(ctx, Options{Host: "evil.test/path", Credentials: credentials(t, env)})
	require.ErrorIs(t, err, gitclient.KindInvalidSource)

	configDir := t.TempDir()
	require.NoError(t, writeHosts(configDir))
	client, err = New(ctx, Options{Host: "known.example.test", Credentials: credentials(t, map[string]string{"GH_CONFIG_DIR": configDir})})
	require.NoError(t, err)
	require.True(t, client.Authenticated(), "a host in gh's hosts.yml is known")
}

func writeHosts(dir string) error {
	return os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte("known.example.test:\n  oauth_token: stored\n"), 0o600)
}

func TestConstructDoesNotNeedSubprocess(t *testing.T) {
	t.Setenv("PATH", "")
	client, err := New(ctx, Options{Credentials: credentials(t, map[string]string{"GH_TOKEN": "t"})})
	require.NoError(t, err)
	require.NotNil(t, client)
}
