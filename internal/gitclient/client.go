package gitclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

const (
	originName     = "origin"
	fetchedRefRoot = "refs/nosey/"
	infiniteDepth  = 0x7fffffff
)

// Options configure a Client.
type Options struct {
	// Policy is the transport policy; the zero value is Hardened.
	Policy Policy
	// Credentials resolves authentication; nil uses the operating system's.
	Credentials *Credentials
	// TempDir is the parent of temporary repositories; empty uses os.TempDir.
	TempDir string
	// Limits bound tree materialization and listing; zero fields use defaults.
	Limits Limits
}

// Client performs Git operations under one policy.
type Client struct {
	policy      Policy
	credentials *Credentials
	tempDir     string
	limits      Limits
}

// New returns a Client. It performs no I/O.
func New(options Options) *Client {
	credentials := options.Credentials
	if credentials == nil {
		credentials = &Credentials{}
	}
	return &Client{policy: options.Policy, credentials: credentials, tempDir: options.TempDir, limits: options.Limits.withDefaults()}
}

func (c *Client) auth(ctx context.Context, op string, remote target, required bool) (transport.AuthMethod, error) {
	return c.credentials.auth(ctx, op, remote, required)
}

// Repo is a Git repository: a temporary one owned by this package, or the user's.
type Repo struct {
	client    *Client
	repo      *git.Repository
	dir       string
	temporary bool
	worktree  bool
	closed    bool
	remote    *target
	// objects is a user repository's object directory.
	objects string
	// fullHistory keeps a temporary repository's fetches from being shallow.
	fullHistory bool
}

// TemporaryOptions configure a temporary repository.
type TemporaryOptions struct {
	// Worktree creates a non-bare repository for commit workflows.
	Worktree bool
}

// Temporary creates a private repository with origin set to remote. The remote is
// validated first, so an invalid source leaves nothing behind. Close removes it.
func (c *Client) Temporary(ctx context.Context, remote string, options TemporaryOptions) (*Repo, error) {
	resolved, err := c.policy.resolve(remote)
	if err != nil {
		return nil, err
	}
	return c.temporary(ctx, resolved, options)
}

func (c *Client) temporary(ctx context.Context, resolved target, options TemporaryOptions) (*Repo, error) {
	const op = "create temporary repository"
	if err := ctx.Err(); err != nil {
		return nil, newError(KindCanceled, op, "", err)
	}
	dir, err := os.MkdirTemp(c.tempDir, "nosey-git-")
	if err != nil {
		return nil, newError(KindFailed, op, "", err)
	}
	repo, err := git.PlainInit(dir, !options.Worktree)
	if err == nil {
		_, err = repo.CreateRemote(&config.RemoteConfig{Name: originName, URLs: []string{resolved.url}})
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, classify(ctx, op, "", err)
	}
	return &Repo{client: c, repo: repo, dir: dir, temporary: true, worktree: options.Worktree, remote: &resolved}, nil
}

// Close removes a temporary repository. It is idempotent and does nothing for a
// user repository.
func (r *Repo) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	if !r.temporary {
		return nil
	}
	return os.RemoveAll(r.dir)
}

// Dir returns the repository's root directory.
func (r *Repo) Dir() string { return r.dir }

// RemoteRef is a resolved remote ref. Commit is the peeled commit; Name is empty
// for a requested exact SHA.
type RemoteRef struct {
	Name   string
	Commit string
	// Branch is the short branch name when Name is HEAD or a branch.
	Branch string
	// object is the advertised, unpeeled object: a tag object for annotated tags.
	object string
}

// ResolveRemote resolves ref on remote without creating a repository.
func (c *Client) ResolveRemote(ctx context.Context, remote, ref string) (RemoteRef, error) {
	resolved, err := c.policy.resolve(remote)
	if err != nil {
		return RemoteRef{}, err
	}
	return c.resolveRemote(ctx, resolved, ref)
}

// Resolve resolves ref against this repository's origin. An exact SHA is
// returned as-is without contacting the remote; FetchRef proves it exists.
func (r *Repo) Resolve(ctx context.Context, ref string) (RemoteRef, error) {
	remote, err := r.origin()
	if err != nil {
		return RemoteRef{}, err
	}
	return r.client.resolveRemote(ctx, remote, ref)
}

func (c *Client) resolveRemote(ctx context.Context, remote target, ref string) (RemoteRef, error) {
	const op = "resolve ref"
	if err := checkRef(op, ref); err != nil {
		return RemoteRef{}, err
	}
	if sha1Pattern.MatchString(ref) {
		return RemoteRef{Commit: strings.ToLower(ref), object: strings.ToLower(ref)}, nil
	}
	refs, err := c.list(ctx, op, remote)
	if err != nil {
		return RemoteRef{}, err
	}
	found, ok := refs.lookup(ref)
	if !ok {
		return RemoteRef{}, newError(KindMissingRef, op, ref, nil)
	}
	return found, nil
}

// advertised is one listing of a remote's refs.
type advertised struct {
	hashes map[string]string
	peeled map[string]string
	head   string
}

func (c *Client) list(ctx context.Context, op string, remote target) (advertised, error) {
	auth, err := c.auth(ctx, op, remote, false)
	if err != nil {
		return advertised{}, err
	}
	lister := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: originName, URLs: []string{remote.url}})
	refs, err := lister.ListContext(ctx, &git.ListOptions{Auth: auth, PeelingOption: git.AppendPeeled})
	if retriesAnonymously(auth, err) {
		if anonymous, retryErr := lister.ListContext(ctx, &git.ListOptions{PeelingOption: git.AppendPeeled}); retryErr == nil {
			refs, err = anonymous, nil
		}
	}
	if err != nil {
		return advertised{}, classify(ctx, op, "", err)
	}
	result := advertised{hashes: map[string]string{}, peeled: map[string]string{}}
	for _, ref := range refs {
		name := ref.Name().String()
		if ref.Type() == plumbing.SymbolicReference {
			if name == "HEAD" {
				result.head = ref.Target().String()
			}
			continue
		}
		if base, ok := strings.CutSuffix(name, "^{}"); ok {
			result.peeled[base] = ref.Hash().String()
			continue
		}
		result.hashes[name] = ref.Hash().String()
	}
	if result.head != "" {
		if hash, ok := result.hashes[result.head]; ok {
			result.hashes["HEAD"] = hash
		}
	}
	return result, nil
}

// lookup applies Git's ref disambiguation for a remote: exact, refs/x,
// refs/tags/x, then refs/heads/x.
func (a advertised) lookup(ref string) (RemoteRef, bool) {
	if ref == "" {
		ref = "HEAD"
	}
	for _, candidate := range []string{ref, "refs/" + ref, "refs/tags/" + ref, "refs/heads/" + ref} {
		hash, ok := a.hashes[candidate]
		if !ok {
			continue
		}
		found := RemoteRef{Name: candidate, Commit: hash, object: hash}
		if peeled, isTag := a.peeled[candidate]; isTag {
			found.Commit = peeled
		}
		if branch, isBranch := strings.CutPrefix(candidate, "refs/heads/"); isBranch {
			found.Branch = branch
		} else if candidate == "HEAD" {
			found.Branch = strings.TrimPrefix(a.head, "refs/heads/")
		}
		return found, true
	}
	return RemoteRef{}, false
}

// tipName returns an advertised ref whose commit is sha.
func (a advertised) tipName(sha string) (string, bool) {
	for name, hash := range a.hashes {
		if hash == sha && name != "HEAD" {
			return name, true
		}
	}
	for name, hash := range a.peeled {
		if hash == sha {
			return name, true
		}
	}
	if a.hashes["HEAD"] == sha {
		return "HEAD", true
	}
	return "", false
}

func (r *Repo) origin() (target, error) {
	if r.remote != nil {
		return *r.remote, nil
	}
	url, err := r.originRaw()
	if err != nil {
		return target{}, err
	}
	if url == "" {
		return target{}, invalid("origin", "repository has no origin remote")
	}
	resolved, err := r.client.policy.resolve(url)
	if err != nil {
		return target{}, err
	}
	r.remote = &resolved
	return resolved, nil
}

func (r *Repo) fetch(ctx context.Context, op string, remote target, depth int, specs ...config.RefSpec) error {
	auth, err := r.client.auth(ctx, op, remote, false)
	if err != nil {
		return err
	}
	handle, err := r.repo.Remote(originName)
	if err != nil {
		return classify(ctx, op, "", err)
	}
	options := &git.FetchOptions{RemoteURL: remote.url, Auth: auth, RefSpecs: specs, Depth: depth, Tags: git.NoTags, Force: true}
	fetchOnce := func() error {
		return retryOnHangup(ctx, func() error { return handle.FetchContext(ctx, options) })
	}
	err = fetchOnce()
	if retriesAnonymously(auth, err) {
		options.Auth = nil
		// Report the original failure when the anonymous attempt fails too: a host
		// may answer 404 anonymously for a private repository, which is not a missing one.
		if retryErr := fetchOnce(); retryErr == nil || errors.Is(retryErr, git.NoErrAlreadyUpToDate) {
			return nil
		}
	}
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// retriesAnonymously reports whether a read rejected for a configured HTTP
// credential should be tried once without it, so a stale token cannot break
// access to a public repository.
func retriesAnonymously(auth transport.AuthMethod, err error) bool {
	if _, isToken := auth.(*githttp.BasicAuth); !isToken {
		return false
	}
	return errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed)
}

func (r *Repo) defaultDepth() int {
	if r.temporary && !r.fullHistory {
		return 1
	}
	return 0
}

// FetchRef fetches ref (HEAD, a branch, a tag or an exact SHA) and returns its
// commit. A temporary repository fetches shallowly; a user repository keeps its
// full history so its clone is not turned shallow.
//
// Do not mix FetchRef and FetchBlobless of one commit in one repository: an
// already-present commit is not fetched again, so blobs would remain missing.
func (r *Repo) FetchRef(ctx context.Context, ref string) (string, error) {
	const op = "fetch"
	if err := checkRef(op, ref); err != nil {
		return "", err
	}
	remote, err := r.origin()
	if err != nil {
		return "", err
	}
	if sha1Pattern.MatchString(ref) {
		return r.fetchExact(ctx, remote, strings.ToLower(ref))
	}
	found, err := r.client.resolveRemote(ctx, remote, ref)
	if err != nil {
		return "", err
	}
	return r.fetchNamed(ctx, remote, found, ref)
}

func (r *Repo) fetchNamed(ctx context.Context, remote target, found RemoteRef, ref string) (string, error) {
	const op = "fetch"
	destination := plumbing.ReferenceName(fetchedRefRoot + "fetch/" + strings.TrimPrefix(found.Name, "refs/"))
	defer r.dropScratchRef(destination)
	spec := config.RefSpec("+" + found.Name + ":" + destination.String())
	if err := r.fetch(ctx, op, remote, r.defaultDepth(), spec); err != nil {
		return "", classify(ctx, op, ref, err)
	}
	stored, err := r.repo.Reference(destination, true)
	if err != nil {
		return "", classify(ctx, op, ref, err)
	}
	return r.peel(ctx, op, ref, stored.Hash())
}

func (r *Repo) dropScratchRef(name plumbing.ReferenceName) {
	if !r.temporary {
		_ = r.repo.Storer.RemoveReference(name)
	}
}

// peel returns the commit hash for an object that may be an annotated tag.
func (r *Repo) peel(ctx context.Context, op, ref string, hash plumbing.Hash) (string, error) {
	if tag, err := r.repo.TagObject(hash); err == nil {
		commit, commitErr := tag.Commit()
		if commitErr != nil {
			return "", newError(KindMissingRef, op, ref, commitErr)
		}
		return commit.Hash.String(), nil
	}
	commit, err := r.repo.CommitObject(hash)
	if err != nil {
		return "", classify(ctx, op, ref, err)
	}
	return commit.Hash.String(), nil
}

// fetchExact fetches sha. When the server does not advertise exact-SHA wants it
// falls back to the advertised ref at that commit, or to a full fetch of branches
// and tags, and then requires that exact commit. Another revision is never
// substituted.
func (r *Repo) fetchExact(ctx context.Context, remote target, sha string) (string, error) {
	const op = "fetch"
	destination := plumbing.ReferenceName(fetchedRefRoot + "exact/" + sha)
	defer r.dropScratchRef(destination)
	spec := config.RefSpec("+" + sha + ":" + destination.String())
	err := r.fetch(ctx, op, remote, r.defaultDepth(), spec)
	if errors.Is(err, git.ErrExactSHA1NotSupported) {
		err = r.fetchWithoutExact(ctx, op, remote, sha)
	}
	if err != nil {
		return "", classify(ctx, op, sha, err)
	}
	return r.verifyCommit(ctx, op, sha)
}

func (r *Repo) fetchWithoutExact(ctx context.Context, op string, remote target, sha string) error {
	refs, err := r.client.list(ctx, op, remote)
	if err != nil {
		return err
	}
	if name, ok := refs.tipName(sha); ok {
		destination := fetchedRefRoot + "tip/" + strings.TrimPrefix(name, "refs/")
		defer r.dropScratchRef(plumbing.ReferenceName(destination))
		return r.fetch(ctx, op, remote, r.defaultDepth(), config.RefSpec("+"+name+":"+destination))
	}
	heads, tags := fetchedRefRoot+"all/heads/", fetchedRefRoot+"all/tags/"
	if err = r.fetch(ctx, op, remote, r.completeDepth(),
		config.RefSpec("+refs/heads/*:"+heads+"*"),
		config.RefSpec("+refs/tags/*:"+tags+"*"),
	); err != nil {
		return err
	}
	if !r.temporary {
		r.dropScratchRefs(heads, tags)
	}
	return nil
}

// completeDepth is the depth that fetches a ref's whole history. It is zero
// unless earlier shallow fetches left commits here: a depth-less fetch then
// advertises them as complete and the server omits their ancestors, so a shallow
// repository asks for unlimited depth instead, as git fetch --unshallow does.
func (r *Repo) completeDepth() int {
	if shallow, err := r.repo.Storer.Shallow(); err == nil && len(shallow) > 0 {
		return infiniteDepth
	}
	return 0
}

func (r *Repo) dropScratchRefs(prefixes ...string) {
	iter, err := r.repo.References()
	if err != nil {
		return
	}
	defer iter.Close()
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		for _, prefix := range prefixes {
			if strings.HasPrefix(ref.Name().String(), prefix) {
				_ = r.repo.Storer.RemoveReference(ref.Name())
			}
		}
		return nil
	})
}

func (r *Repo) verifyCommit(ctx context.Context, op, sha string) (string, error) {
	if _, err := r.repo.CommitObject(plumbing.NewHash(sha)); err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return "", newError(KindMissingRef, op, sha, err)
		}
		return "", classify(ctx, op, sha, err)
	}
	return sha, nil
}

// HasCommit reports whether sha is present locally.
func (r *Repo) HasCommit(sha string) bool {
	if !sha1Pattern.MatchString(sha) {
		return false
	}
	_, err := r.repo.CommitObject(plumbing.NewHash(sha))
	return err == nil
}

// FetchBlobless fetches ref's commit with trees but no blobs, for listing and
// comparing trees. When the server lacks partial clone or exact-SHA support it
// falls back to FetchRef.
func (r *Repo) FetchBlobless(ctx context.Context, ref string) (string, error) {
	const op = "blobless fetch"
	if err := checkRef(op, ref); err != nil {
		return "", err
	}
	remote, err := r.origin()
	if err != nil {
		return "", err
	}
	auth, err := r.client.auth(ctx, op, remote, false)
	if err != nil {
		return "", err
	}
	transportClient, err := client.NewClient(remote.endpoint)
	if err != nil {
		return "", classify(ctx, op, ref, err)
	}
	var object string
	err = retryOnHangup(ctx, func() error {
		var attemptErr error
		object, attemptErr = r.bloblessAttempt(ctx, transportClient, remote, auth, ref)
		return attemptErr
	})
	if errors.Is(err, errNeedsFallback) {
		return r.FetchRef(ctx, ref)
	}
	if err != nil {
		return "", classify(ctx, op, ref, err)
	}
	return r.peel(ctx, op, ref, plumbing.NewHash(object))
}

// bloblessAttempt runs one session, returning errNeedsFallback when the server
// cannot serve this fetch without a regular FetchRef.
func (r *Repo) bloblessAttempt(ctx context.Context, transportClient transport.Transport, remote target, auth transport.AuthMethod, ref string) (string, error) {
	session, advertisement, err := advertisedRefs(ctx, transportClient, remote, auth)
	if retriesAnonymously(auth, err) {
		// As in fetch, the credential's failure is what is reported if this fails too.
		if anonymous, adv, retryErr := advertisedRefs(ctx, transportClient, remote, nil); retryErr == nil {
			session, advertisement, err = anonymous, adv, nil
		}
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()
	if !advertisement.Capabilities.Supports(capability.Filter) {
		return "", errNeedsFallback
	}
	found, err := r.bloblessTarget(ctx, advertisement, remote, ref)
	if err != nil {
		return "", err
	}
	return found.object, r.uploadBlobless(ctx, session, advertisement, found.object)
}

func advertisedRefs(ctx context.Context, transportClient transport.Transport, remote target, auth transport.AuthMethod) (transport.UploadPackSession, *packp.AdvRefs, error) {
	session, err := transportClient.NewUploadPackSession(remote.endpoint, auth)
	if err != nil {
		return nil, nil, err
	}
	advertisement, err := session.AdvertisedReferencesContext(ctx)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	return session, advertisement, nil
}

var errNeedsFallback = errors.New("exact SHA wants are not supported")

func (r *Repo) bloblessTarget(ctx context.Context, advertisement *packp.AdvRefs, remote target, ref string) (RemoteRef, error) {
	if !sha1Pattern.MatchString(ref) {
		return r.client.resolveRemote(ctx, remote, ref)
	}
	sha := strings.ToLower(ref)
	if advertisement.Capabilities.Supports(capability.AllowTipSHA1InWant) || advertisement.Capabilities.Supports(capability.AllowReachableSHA1InWant) {
		return RemoteRef{Commit: sha, object: sha}, nil
	}
	for _, hash := range advertisement.References {
		if hash.String() == sha {
			return RemoteRef{Commit: sha, object: sha}, nil
		}
	}
	return RemoteRef{}, errNeedsFallback
}

func (r *Repo) uploadBlobless(ctx context.Context, session transport.UploadPackSession, advertisement *packp.AdvRefs, object string) error {
	request := packp.NewUploadPackRequestFromCapabilities(advertisement.Capabilities)
	request.Wants = []plumbing.Hash{plumbing.NewHash(object)}
	request.Depth = packp.DepthCommits(1)
	// Without sideband the response body is the raw packfile UpdateObjectStorage reads.
	request.Capabilities.Delete(capability.Sideband64k)
	request.Capabilities.Delete(capability.Sideband)
	if err := request.Capabilities.Set(capability.Filter); err != nil {
		return err
	}
	if advertisement.Capabilities.Supports(capability.Shallow) {
		if err := request.Capabilities.Set(capability.Shallow); err != nil {
			return err
		}
	}
	request.Filter = packp.FilterBlobNone()
	response, err := session.UploadPack(ctx, request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Close() }()
	if err = r.mergeShallow(response.Shallows); err != nil {
		return err
	}
	return packfile.UpdateObjectStorage(r.repo.Storer, response)
}

func (r *Repo) mergeShallow(added []plumbing.Hash) error {
	if len(added) == 0 {
		return nil
	}
	shallow, ok := r.repo.Storer.(storer.ShallowStorer)
	if !ok {
		return nil
	}
	current, err := shallow.Shallow()
	if err != nil {
		return err
	}
	return shallow.SetShallow(append(current, added...))
}

func (r *Repo) commitTree(ctx context.Context, op, sha string) (*object.Tree, error) {
	if err := ctx.Err(); err != nil {
		return nil, newError(KindCanceled, op, sha, err)
	}
	if sha256Pattern.MatchString(sha) {
		return nil, newError(KindUnsupportedFormat, op, sha, nil)
	}
	if !sha1Pattern.MatchString(sha) {
		return nil, invalid(op, "not a full commit hash: %q", sha)
	}
	commit, err := r.repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return nil, classify(ctx, op, sha, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, classify(ctx, op, sha, err)
	}
	return tree, nil
}

// ReadFile returns the bytes at path in commit, at most limit bytes (zero uses
// the MaxFileBytes limit). A symlink returns its target text, like git show.
func (r *Repo) ReadFile(ctx context.Context, commit, path string, limit int64) ([]byte, error) {
	const op = "read file"
	tree, err := r.commitTree(ctx, op, commit)
	if err != nil {
		return nil, err
	}
	entry, err := tree.FindEntry(path)
	if err != nil {
		return nil, newError(KindMissingPath, op, path, err)
	}
	if entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule {
		return nil, newError(KindMissingPath, op, path, fmt.Errorf("not a file"))
	}
	if limit <= 0 {
		limit = r.client.limits.MaxFileBytes
	}
	return r.readBlob(ctx, op, path, entry.Hash, limit)
}

func (r *Repo) readBlob(ctx context.Context, op, path string, hash plumbing.Hash, limit int64) ([]byte, error) {
	blob, err := r.repo.BlobObject(hash)
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, newError(KindMissingBlob, op, path, err)
		}
		return nil, classify(ctx, op, path, err)
	}
	if blob.Size > limit {
		return nil, newError(KindResourceLimit, op, path, fmt.Errorf("%d bytes exceeds the %d byte limit", blob.Size, limit))
	}
	reader, err := blob.Reader()
	if err != nil {
		return nil, classify(ctx, op, path, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: reader}, limit+1))
	if err != nil {
		return nil, classify(ctx, op, path, err)
	}
	if int64(len(data)) > limit {
		return nil, newError(KindResourceLimit, op, path, fmt.Errorf("exceeds the %d byte limit", limit))
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (c contextReader) Read(buffer []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.reader.Read(buffer)
}

// ListFiles returns the slash-separated paths of regular files and symlinks in
// commit, bounded by MaxFiles. Submodule entries are excluded.
func (r *Repo) ListFiles(ctx context.Context, commit string) ([]string, error) {
	const op = "list files"
	tree, err := r.commitTree(ctx, op, commit)
	if err != nil {
		return nil, err
	}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	files := []string{}
	for {
		if err = ctx.Err(); err != nil {
			return nil, newError(KindCanceled, op, commit, err)
		}
		name, entry, nextErr := walker.Next()
		if errors.Is(nextErr, io.EOF) {
			return files, nil
		}
		if nextErr != nil {
			return nil, classify(ctx, op, commit, nextErr)
		}
		if !isFileMode(entry.Mode) {
			continue
		}
		files = append(files, name)
		if int64(len(files)) > r.client.limits.MaxFiles {
			return nil, newError(KindResourceLimit, op, commit, fmt.Errorf("more than %d files", r.client.limits.MaxFiles))
		}
	}
}

func isFileMode(mode filemode.FileMode) bool {
	switch mode {
	case filemode.Regular, filemode.Deprecated, filemode.Executable, filemode.Symlink:
		return true
	}
	return false
}

// TreeHash returns the object name of path at commit; an empty path names the
// root tree.
func (r *Repo) TreeHash(ctx context.Context, commit, path string) (string, error) {
	const op = "tree hash"
	tree, err := r.commitTree(ctx, op, commit)
	if err != nil {
		return "", err
	}
	if path == "" {
		return tree.Hash.String(), nil
	}
	entry, err := tree.FindEntry(path)
	if err != nil {
		return "", newError(KindMissingPath, op, path, err)
	}
	return entry.Hash.String(), nil
}

// ExportRef fetches ref from remote and materializes it into dest, always
// removing its temporary repository. It returns the commit.
func (c *Client) ExportRef(ctx context.Context, remote, ref, dest string) (string, error) {
	repo, err := c.Temporary(ctx, remote, TemporaryOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = repo.Close() }()
	commit, err := repo.FetchRef(ctx, ref)
	if err != nil {
		return "", err
	}
	if err = repo.Materialize(ctx, commit, dest); err != nil {
		return "", err
	}
	return commit, nil
}
