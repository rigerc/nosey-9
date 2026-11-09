package registry

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

const originURL = "https://github.com/owner/repo"

// registryOrigin is a Git repository holding a valid registry, reached through
// the owner/repo shorthand. Any other URL, or going offline, dials a missing path.
type registryOrigin struct {
	t       *testing.T
	dir     string
	client  *gitclient.Client
	offline *atomic.Bool
	head    string
}

func newRegistryOrigin(t *testing.T) *registryOrigin {
	t.Helper()
	dir := fixtureRegistry(t, `{"schemaVersion":2,"technologies":[]}`, `{"schemaVersion":2,"skills":[]}`, `{"schemaVersion":2,"mappings":[]}`)
	origin := &registryOrigin{t: t, dir: dir, offline: &atomic.Bool{}}
	missing := "file://" + filepath.Join(t.TempDir(), "missing.git")
	rewrite := func(url string) string {
		if url != originURL || origin.offline.Load() {
			return missing
		}
		return "file://" + dir
	}
	home := t.TempDir()
	origin.client = gitclient.New(gitclient.Options{
		Policy: gitclient.Policy{AllowFile: true, Rewrite: rewrite},
		Credentials: &gitclient.Credentials{
			Getenv:  func(string) string { return "" },
			HomeDir: func() (string, error) { return home, nil },
			Keyring: func(string, string) (string, error) { return "", errors.New("no keyring") },
		},
		TempDir: t.TempDir(),
	})
	origin.git("init", "-q", "-b", "main")
	origin.commit(nil)
	return origin
}

func (o *registryOrigin) git(args ...string) string {
	o.t.Helper()
	// #nosec G204 -- test helper with a fixed binary and test-controlled arguments.
	command := exec.Command("git", args...)
	command.Dir = o.dir
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := command.CombinedOutput()
	if err != nil {
		o.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (o *registryOrigin) commit(files map[string]string) string {
	o.t.Helper()
	for name, data := range files {
		if err := storage.Write(filepath.Join(o.dir, name), []byte(data)); err != nil {
			o.t.Fatal(err)
		}
	}
	o.git("add", "-A")
	o.git("commit", "-q", "--allow-empty", "-m", "change")
	o.head = o.git("rev-parse", "HEAD")
	return o.head
}

func (o *registryOrigin) ref(revision string) domain.RegistryRef {
	return domain.RegistryRef{ID: "test:registry", Source: domain.RegistrySource{Kind: "git", URI: "owner/repo", Ref: revision}, Enabled: true}
}

func TestNativeRegistryRefreshOfflineFallback(t *testing.T) {
	origin := newRegistryOrigin(t)
	service := Service{Store: settings.Store{Cache: t.TempDir()}, Git: origin.client}
	ctx := context.Background()

	origin.offline.Store(true)
	if _, err := service.Resolve(ctx, origin.ref(""), 24, false); !errors.Is(err, gitclient.KindMissingRepository) {
		t.Fatalf("no cache offline: %v", err)
	}

	origin.offline.Store(false)
	first, err := service.Resolve(ctx, origin.ref(""), 24, false)
	if err != nil || first.Stale || *first.Revision != origin.head {
		t.Fatal(first, err)
	}
	pointer := filepath.Join(filepath.Dir(first.Root), "current.json")
	before, err := os.ReadFile(pointer) //nolint:gosec // test fixture under t.TempDir
	if err != nil {
		t.Fatal(err)
	}

	origin.offline.Store(true)
	fresh, err := service.Resolve(ctx, origin.ref(""), 24, false)
	if err != nil || fresh.Stale || fresh.Root != first.Root {
		t.Fatal("a fresh cache must not need the network", fresh, err)
	}
	stale, err := service.Resolve(ctx, origin.ref(""), 24, true)
	if err != nil || !stale.Stale || stale.Root != first.Root {
		t.Fatal(stale, err)
	}
	after, err := os.ReadFile(pointer) //nolint:gosec // test fixture under t.TempDir
	if err != nil || string(after) != string(before) {
		t.Fatal("a failed refresh must not renew the cache", err)
	}
	entries, err := os.ReadDir(filepath.Dir(first.Root))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "fetch-") {
			t.Fatal("failed refresh left a stage behind:", entry.Name())
		}
	}
}

func TestNativeSnapshotSelectsImmutableRevision(t *testing.T) {
	origin := newRegistryOrigin(t)
	pinned := origin.head
	origin.git("tag", "v1")
	latest := origin.commit(map[string]string{"marker": "new"})
	service := Service{Store: settings.Store{Cache: t.TempDir()}, Git: origin.client}
	for ref, want := range map[string]string{"": latest, "main": latest, "v1": pinned, pinned: pinned} {
		snap, err := service.Resolve(context.Background(), origin.ref(ref), 24, true)
		if err != nil {
			t.Fatal(ref, err)
		}
		if *snap.Revision != want || filepath.Base(snap.Root) != want {
			t.Fatalf("ref %q resolved to %s, want %s", ref, *snap.Revision, want)
		}
		if _, err = os.Stat(filepath.Join(snap.Root, "marker")); (want == latest) != (err == nil) {
			t.Fatalf("ref %q materialized the wrong tree: %v", ref, err)
		}
	}
	if _, err := service.Resolve(context.Background(), origin.ref(strings.Repeat("0", 40)), 24, true); !errors.Is(err, gitclient.KindMissingRef) {
		t.Fatal("absent commit:", err)
	}
}

func TestNativeSnapshotSymlinksFollowValidation(t *testing.T) {
	origin := newRegistryOrigin(t)
	if err := os.Symlink("../../outside", filepath.Join(origin.dir, "stray")); err != nil {
		t.Fatal(err)
	}
	origin.commit(nil)
	service := Service{Store: settings.Store{Cache: t.TempDir()}, Git: origin.client}
	if _, err := service.Resolve(context.Background(), origin.ref(""), 24, true); err != nil {
		t.Fatal("an undeclared escaping symlink must not fail the snapshot:", err)
	}
	if err := os.Remove(filepath.Join(origin.dir, "skills.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../outside.json", filepath.Join(origin.dir, "skills.json")); err != nil {
		t.Fatal(err)
	}
	escaping := origin.commit(nil)
	stale, err := service.Resolve(context.Background(), origin.ref(""), 24, true)
	if err != nil || !stale.Stale || *stale.Revision == escaping {
		t.Fatal("a declared file escaping the tree must fail validation", stale, err)
	}
}
