package registry

import (
	"context"
	"net/http/cgi" //nolint:gosec // the test fixture serves git-http-backend; Httpoxy does not apply
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/settings"
)

// TestNativePublicRegistryRefresh resolves and refreshes a registry over smart
// HTTP with PATH emptied. The fixture's git-http-backend is started by absolute
// path outside the Nosey process; the file transport could not run here.
func TestNativePublicRegistryRefresh(t *testing.T) {
	origin := newRegistryOrigin(t)
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	if _, err = os.Stat(backend); err != nil {
		t.Skip("git-http-backend is not installed")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "registry.git")
	origin.git("clone", "-q", "--bare", origin.dir, bare)
	pushToBare := func() { origin.git("push", "-q", bare, "HEAD:refs/heads/main") }
	pushToBare()
	originalPath := os.Getenv("PATH")
	server := httptest.NewServer(&cgi.Handler{Path: backend, Root: "/", Env: []string{
		"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"PATH=" + originalPath,
	}})
	defer server.Close()
	home := t.TempDir()
	client := gitclient.New(gitclient.Options{
		Policy:      gitclient.Policy{AllowHTTP: true, Rewrite: func(string) string { return server.URL + "/registry.git" }},
		Credentials: &gitclient.Credentials{Getenv: func(string) string { return "" }, HomeDir: func() (string, error) { return home, nil }},
		TempDir:     t.TempDir(),
	})
	service := Service{Store: settings.Store{Cache: t.TempDir()}, Git: client}
	ref := domain.RegistryRef{ID: "test:registry", Source: domain.RegistrySource{Kind: "git", URI: originURL}, Enabled: true}

	t.Setenv("PATH", "")
	first, err := service.Resolve(context.Background(), ref, 24, true)
	if err != nil || first.Stale || *first.Revision != origin.head {
		t.Fatal(first, err)
	}

	// The fixture needs git to advance the upstream; the refresh that follows does not.
	t.Setenv("PATH", originalPath)
	head := origin.commit(map[string]string{"marker": "new"})
	pushToBare()
	t.Setenv("PATH", "")
	second, err := service.Resolve(context.Background(), ref, 24, true)
	if err != nil || second.Stale || *second.Revision != head || second.Root == first.Root {
		t.Fatal(second, err)
	}
	if _, err = os.Stat(filepath.Join(second.Root, "marker")); err != nil {
		t.Fatal("the refreshed snapshot lacks the new tree:", err)
	}
}
