package gitclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// TemporaryOrigin creates a temporary repository for this repository's origin,
// keeping any credentials embedded in the origin URL out of its configuration.
func (r *Repo) TemporaryOrigin(ctx context.Context, options TemporaryOptions) (*Repo, error) {
	remote, err := r.origin()
	if err != nil {
		return nil, err
	}
	return r.client.temporary(ctx, remote, options)
}

// ExportOrigin fetches ref from this repository's origin into a temporary
// repository and materializes it into dest, for commits this repository holds
// only partially. Symlinks are written as committed.
func (r *Repo) ExportOrigin(ctx context.Context, ref, dest string) error {
	temporary, err := r.TemporaryOrigin(ctx, TemporaryOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = temporary.Close() }()
	commit, err := temporary.FetchRef(ctx, ref)
	if err != nil {
		return err
	}
	return temporary.MaterializeVerbatimLinks(ctx, commit, dest)
}

// FetchOriginSnapshot fetches ref from origin without writing to this repository.
// The objects land in a temporary bare repository that reads this repository's
// objects through an alternates entry, so only the commits missing here are
// transferred and AheadBehind can compare them. A shallow repository is not
// linked: its partial history cannot be offered to the server as complete, so
// the snapshot is a depth-1 copy and AheadBehind cannot compare it. Close removes
// the snapshot.
func (r *Repo) FetchOriginSnapshot(ctx context.Context, ref string) (*Repo, string, error) {
	const op = "snapshot origin"
	remote, err := r.origin()
	if err != nil {
		return nil, "", err
	}
	snapshot, err := r.client.temporary(ctx, remote, TemporaryOptions{})
	if err != nil {
		return nil, "", err
	}
	shallow, err := r.repo.Storer.Shallow()
	if err != nil {
		_ = snapshot.Close()
		return nil, "", classify(ctx, op, "", err)
	}
	if len(shallow) == 0 {
		if err = snapshot.linkObjects(r); err != nil {
			_ = snapshot.Close()
			return nil, "", classify(ctx, op, "", err)
		}
	}
	commit, err := snapshot.FetchRef(ctx, ref)
	if err != nil {
		_ = snapshot.Close()
		return nil, "", err
	}
	return snapshot, commit, nil
}

// linkObjects makes this temporary repository read user's objects and offer its
// branches to the server as already held.
func (r *Repo) linkObjects(user *Repo) error {
	info := filepath.Join(r.dir, "objects", "info")
	if err := os.MkdirAll(info, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(info, "alternates"), []byte(user.objects+"\n"), 0o600); err != nil {
		return err
	}
	linked, err := git.Open(filesystem.NewStorageWithOptions(osfs.New(r.dir), cache.NewObjectLRUDefault(), filesystem.Options{AlternatesFS: osfs.New("/")}), nil)
	if err != nil {
		return err
	}
	r.repo = linked
	r.fullHistory = true
	return r.seedHaves(user)
}

// seedHaves records user's HEAD and origin-tracking commits as local refs, which
// go-git sends as haves.
func (r *Repo) seedHaves(user *Repo) error {
	const prefix = fetchedRefRoot + "have/"
	head, err := user.Head()
	if err != nil {
		return err
	}
	if head != "" {
		if err = r.repo.Storer.SetReference(plumbing.NewHashReference(prefix+"head", plumbing.NewHash(head))); err != nil {
			return err
		}
	}
	iter, err := user.repo.References()
	if err != nil {
		return err
	}
	defer iter.Close()
	return iter.ForEach(func(ref *plumbing.Reference) error {
		name, isOrigin := strings.CutPrefix(ref.Name().String(), "refs/remotes/origin/")
		if !isOrigin || ref.Type() != plumbing.HashReference {
			return nil
		}
		return r.repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(prefix+"origin/"+name), ref.Hash()))
	})
}

// AheadBehind counts commits reachable only from left and only from right, like
// git rev-list --left-right --count left...right. A missing parent ends the walk
// on that side, as a shallow boundary does.
func (r *Repo) AheadBehind(ctx context.Context, left, right string) (int, int, error) {
	const op = "compare commits"
	leftSet, err := r.ancestors(ctx, op, left)
	if err != nil {
		return 0, 0, err
	}
	rightSet, err := r.ancestors(ctx, op, right)
	if err != nil {
		return 0, 0, err
	}
	ahead, behind := 0, 0
	for hash := range leftSet {
		if _, shared := rightSet[hash]; !shared {
			ahead++
		}
	}
	for hash := range rightSet {
		if _, shared := leftSet[hash]; !shared {
			behind++
		}
	}
	return ahead, behind, nil
}

func (r *Repo) ancestors(ctx context.Context, op, start string) (map[plumbing.Hash]struct{}, error) {
	if !sha1Pattern.MatchString(start) {
		return nil, invalid(op, "not a full commit hash: %q", start)
	}
	seen := map[plumbing.Hash]struct{}{}
	pending := []plumbing.Hash{plumbing.NewHash(start)}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, newError(KindCanceled, op, start, err)
		}
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, visited := seen[hash]; visited {
			continue
		}
		commit, err := r.repo.CommitObject(hash)
		if errors.Is(err, plumbing.ErrObjectNotFound) && hash.String() != start {
			continue
		}
		if err != nil {
			return nil, classify(ctx, op, hash.String(), err)
		}
		seen[hash] = struct{}{}
		pending = append(pending, commit.ParentHashes...)
	}
	return seen, nil
}

// OriginLocation names a repository on a Git host, parsed from origin's URL
// before any test rewrite. Host carries a non-default HTTP port for Enterprise.
type OriginLocation struct {
	Host  string
	Owner string
	Name  string
}

// OriginLocation parses origin as a two-segment hosted repository. It returns
// false when origin is not shaped like owner/repo on a host.
func (r *Repo) OriginLocation() (OriginLocation, bool, error) {
	remote, err := r.origin()
	if err != nil {
		return OriginLocation{}, false, err
	}
	endpoint, err := transport.NewEndpoint(remote.declared)
	if err != nil || endpoint.Host == "" {
		return OriginLocation{}, false, nil
	}
	parts := strings.Split(strings.TrimSuffix(strings.Trim(endpoint.Path, "/"), ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return OriginLocation{}, false, nil
	}
	host := strings.ToLower(endpoint.Host)
	if (endpoint.Protocol == schemeHTTPS || endpoint.Protocol == schemeHTTP) && endpoint.Port != 0 && !isDefaultPort(endpoint.Protocol, endpoint.Port) {
		host = fmt.Sprintf("%s:%d", host, endpoint.Port)
	}
	return OriginLocation{Host: host, Owner: parts[0], Name: parts[1]}, true, nil
}

func isDefaultPort(protocol string, port int) bool {
	return (protocol == schemeHTTPS && port == 443) || (protocol == schemeHTTP && port == 80)
}
