package gitclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
)

// OpenUser opens the repository containing path, from any subdirectory or linked
// worktree. It tolerates partialClone, worktreeConfig and preciousObjects, which
// do not change how objects and refs are read. SHA-256 and reftable repositories
// yield KindUnsupportedFormat, any other unknown extension fails, and a directory
// outside a repository yields KindMissingRepository.
func (c *Client) OpenUser(path string) (*Repo, error) {
	const op = "open repository"
	location, err := locateRepository(path)
	if err != nil {
		return nil, classifyOpen(op, path, err)
	}
	storage := filesystem.NewStorage(location.dot, cache.NewObjectLRUDefault())
	repo, err := git.Open(tolerantConfig{Storer: storage}, location.worktree)
	if err != nil {
		return nil, classifyOpen(op, path, err)
	}
	// verifyExtensions runs only in Open. Using the real storage from here on means
	// a later config write keeps the extensions the check did not see.
	repo.Storer = userStorage{Storage: storage}
	return &Repo{client: c, repo: repo, dir: location.worktree.Root(), worktree: true, objects: location.objects}, nil
}

// tolerantConfig hides from go-git's Open the extensions that do not change how
// it reads objects and refs. Config returns a freshly parsed value, so the
// removal cannot reach the repository's stored configuration.
type tolerantConfig struct{ storage.Storer }

var tolerableExtensions = []string{"partialclone", "worktreeconfig", "preciousobjects"}

func (t tolerantConfig) Config() (*config.Config, error) {
	cfg, err := t.Storer.Config()
	if err != nil || cfg.Raw == nil || !cfg.Raw.HasSection("extensions") {
		return cfg, err
	}
	section := cfg.Raw.Section("extensions")
	section.Options = slices.DeleteFunc(section.Options, func(option *format.Option) bool {
		return slices.Contains(tolerableExtensions, strings.ToLower(option.Key))
	})
	return cfg, nil
}

// userStorage guards writes to a user repository's configuration.
type userStorage struct{ *filesystem.Storage }

var emptyObjectFormat = regexp.MustCompile(`(?m)^[ \t]*objectformat[ \t]*=[ \t]*\n`)

// SetConfig drops the empty extensions.objectformat line go-git's Marshal emits
// for a format version 1 repository, which Git refuses as an invalid value.
func (s userStorage) SetConfig(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	file, err := s.Filesystem().Create("config")
	if err != nil {
		return err
	}
	if _, err = file.Write(emptyObjectFormat.ReplaceAll(data, nil)); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// repositoryLocation is where one user repository lives on disk.
type repositoryLocation struct {
	dot      billy.Filesystem
	worktree billy.Filesystem
	// objects is the object directory, in the common dir for a linked worktree.
	objects string
}

// locateRepository finds the .git entry at or above path, following a gitdir
// file and a linked worktree's commondir, as git.PlainOpenWithOptions does with
// DetectDotGit and EnableDotGitCommonDir.
func locateRepository(path string) (repositoryLocation, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return repositoryLocation{}, err
	}
	for {
		info, statErr := os.Lstat(filepath.Join(current, git.GitDirName))
		switch {
		case statErr == nil && info.IsDir():
			return resolveDotGit(current, filepath.Join(current, git.GitDirName))
		case statErr == nil && info.Mode().IsRegular():
			gitDir, readErr := readGitFile(current, filepath.Join(current, git.GitDirName))
			if readErr != nil {
				return repositoryLocation{}, readErr
			}
			return resolveDotGit(current, gitDir)
		case statErr != nil && !errors.Is(statErr, os.ErrNotExist) && !errors.Is(statErr, syscall.ENOTDIR):
			return repositoryLocation{}, statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return repositoryLocation{}, git.ErrRepositoryNotExists
		}
		current = parent
	}
}

func readGitFile(root, file string) (string, error) {
	data, err := readBoundedFile(file)
	if err != nil {
		return "", err
	}
	const prefix = "gitdir: "
	line, _, _ := strings.Cut(string(data), "\n")
	gitDir, found := strings.CutPrefix(line, prefix)
	if !found {
		return "", git.ErrRepositoryNotExists
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return gitDir, nil
}

func resolveDotGit(root, gitDir string) (repositoryLocation, error) {
	location := repositoryLocation{worktree: osfs.New(root), objects: filepath.Join(gitDir, "objects")}
	dot := osfs.New(gitDir)
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir")) // #nosec G304 -- inside the user's own Git directory.
	switch {
	case errors.Is(err, os.ErrNotExist):
		location.dot = dot
		return location, nil
	case err != nil:
		return repositoryLocation{}, err
	}
	common := strings.TrimSpace(string(data))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	if _, err = os.Stat(common); err != nil { //nolint:gosec // the path is the user's own repository's commondir, only tested for existence
		return repositoryLocation{}, git.ErrRepositoryIncomplete
	}
	location.dot = dotgit.NewRepositoryFilesystem(dot, osfs.New(common))
	location.objects = filepath.Join(common, "objects")
	return location, nil
}

// classifyOpen separates storage Nosey cannot read from other unknown
// extensions, so callers can rely on KindUnsupportedFormat alone.
func classifyOpen(op, path string, err error) error {
	message := err.Error()
	if strings.Contains(message, "unknown extension") {
		if strings.Contains(message, "objectformat") || strings.Contains(message, "refstorage") {
			return newError(KindUnsupportedFormat, op, path, err)
		}
		return newError(KindFailed, op, path, err)
	}
	return classify(context.Background(), op, path, err)
}

// InitUser initialises a repository at dir with branch as its unborn HEAD,
// opening it instead when one already exists.
func (c *Client) InitUser(dir, branch string) (*Repo, error) {
	const op = "init repository"
	if err := checkBranch(op, branch); err != nil {
		return nil, err
	}
	options := &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(branch)}, Bare: false}
	repo, err := git.PlainInitWithOptions(dir, options)
	if errors.Is(err, git.ErrRepositoryAlreadyExists) {
		return c.OpenUser(dir)
	}
	if err != nil {
		return nil, classify(context.Background(), op, dir, err)
	}
	return &Repo{client: c, repo: repo, dir: dir, worktree: true}, nil
}

// Head returns the commit at HEAD, or "" on an unborn branch.
func (r *Repo) Head() (string, error) {
	head, err := r.repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", nil
	}
	if err != nil {
		return "", classify(context.Background(), "read HEAD", "", err)
	}
	return head.Hash().String(), nil
}

// Branch returns the short branch name of HEAD, or "HEAD" when detached.
func (r *Repo) Branch() (string, error) {
	head, err := r.repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", classify(context.Background(), "read HEAD", "", err)
	}
	if head.Type() == plumbing.SymbolicReference && head.Target().IsBranch() {
		return head.Target().Short(), nil
	}
	return "HEAD", nil
}

// OriginURL returns origin's first URL with any embedded credentials removed, or
// "" when there is no origin.
func (r *Repo) OriginURL() (string, error) {
	raw, err := r.originRaw()
	if err != nil {
		return "", err
	}
	return redactURL(raw), nil
}

// originRaw reads the configured value because go-git applies url.insteadOf to
// its parsed remotes, and that rewrite is deliberately not honoured. The result
// can carry credentials: it never leaves this package.
func (r *Repo) originRaw() (string, error) {
	cfg, err := r.repo.Storer.Config()
	if err != nil {
		return "", classify(context.Background(), "read origin", "", err)
	}
	section := cfg.Raw.Section("remote")
	if !section.HasSubsection(originName) {
		return "", nil
	}
	urls := section.Subsection(originName).Options.GetAll("url")
	if len(urls) == 0 {
		return "", nil
	}
	return urls[0], nil
}

// OriginHead returns the short name of the local refs/remotes/origin/HEAD target,
// such as "origin/main", or "" when it is not set.
func (r *Repo) OriginHead() (string, error) {
	ref, err := r.repo.Reference(plumbing.NewRemoteHEADReferenceName(originName), false)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", nil
	}
	if err != nil {
		return "", classify(context.Background(), "read origin HEAD", "", err)
	}
	if ref.Type() != plumbing.SymbolicReference {
		return "", nil
	}
	return ref.Target().Short(), nil
}

// SetOrigin validates url under the client's policy and writes it as origin.
func (r *Repo) SetOrigin(url string) error {
	const op = "set origin"
	resolved, err := r.client.policy.resolve(url)
	if err != nil {
		return err
	}
	if err = r.repo.DeleteRemote(originName); err != nil && !errors.Is(err, git.ErrRemoteNotFound) {
		return classify(context.Background(), op, "", err)
	}
	if _, err = r.repo.CreateRemote(&config.RemoteConfig{Name: originName, URLs: []string{resolved.declared}}); err != nil {
		return classify(context.Background(), op, "", err)
	}
	r.remote = &resolved
	return nil
}

// SetUpstream records branch as tracking origin.
func (r *Repo) SetUpstream(branch string) error {
	const op = "set upstream"
	if err := checkBranch(op, branch); err != nil {
		return err
	}
	if err := r.repo.DeleteBranch(branch); err != nil && !errors.Is(err, git.ErrBranchNotFound) {
		return classify(context.Background(), op, branch, err)
	}
	err := r.repo.CreateBranch(&config.Branch{Name: branch, Remote: originName, Merge: plumbing.NewBranchReferenceName(branch)})
	return classify(context.Background(), op, branch, err)
}

// Identity is a commit author and committer.
type Identity struct {
	Name  string
	Email string
}

// Identity reads user.name and user.email from repository config, then global
// config. It yields KindIdentityMissing when either stays unset. It does not
// follow include directives and ignores GIT_AUTHOR_* variables.
func (r *Repo) Identity() (Identity, error) {
	const op = "read commit identity"
	identity := Identity{}
	local, err := r.repo.ConfigScoped(config.LocalScope)
	if err == nil {
		identity = Identity{Name: local.User.Name, Email: local.User.Email}
	}
	for _, path := range globalConfigPaths() {
		if identity.Name != "" && identity.Email != "" {
			break
		}
		cfg := readConfigFile(path)
		if cfg == nil {
			continue
		}
		if identity.Name == "" {
			identity.Name = cfg.User.Name
		}
		if identity.Email == "" {
			identity.Email = cfg.User.Email
		}
	}
	if identity.Name == "" || identity.Email == "" {
		return Identity{}, newError(KindIdentityMissing, op, "", errors.New("set user.name and user.email with git config"))
	}
	return identity, nil
}

// globalConfigPaths lists Git's global files by precedence: ~/.gitconfig wins
// over the XDG file.
func globalConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{filepath.Join(home, ".gitconfig")}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "git", "config"))
	}
	return append(paths, filepath.Join(home, ".config", "git", "config"))
}

func readConfigFile(path string) *config.Config {
	// #nosec G304 -- a fixed Git configuration location.
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	cfg, err := config.ReadConfig(file)
	if err != nil {
		return nil
	}
	return cfg
}

// PrepareWorktree materializes commit into a temporary worktree repository and
// makes it HEAD (detached) with a matching index, ready for edits and Commit.
func (r *Repo) PrepareWorktree(ctx context.Context, commit string) error {
	const op = "prepare worktree"
	if !r.temporary || !r.worktree {
		return invalid(op, "PrepareWorktree needs a temporary worktree repository")
	}
	if err := r.materialize(ctx, commit, r.dir, materializeMode{keepGit: true, verbatimLinks: true}); err != nil {
		return err
	}
	hash := plumbing.NewHash(commit)
	if err := r.repo.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, hash)); err != nil {
		return classify(ctx, op, commit, err)
	}
	worktree, err := r.repo.Worktree()
	if err != nil {
		return classify(ctx, op, commit, err)
	}
	return classify(ctx, op, commit, worktree.Reset(&git.ResetOptions{Mode: git.MixedReset, Commit: hash}))
}

// CommitOptions describe a commit.
type CommitOptions struct {
	Message string
	// Paths limits the commit to these files or directories; empty stages every
	// change, including deletions, like git add -A.
	Paths []string
	// Identity overrides Repo.Identity.
	Identity Identity
}

// Commit stages changes and commits them, without hooks or signing. It yields
// KindNoChanges when nothing differs from HEAD, and returns the new commit. Like
// git add, staging normalizes text paths to LF according to .gitattributes and
// leaves the working file as it is; ignored untracked files are not staged.
func (r *Repo) Commit(ctx context.Context, options CommitOptions) (string, error) {
	const op = "commit"
	identity := options.Identity
	if identity.Name == "" || identity.Email == "" {
		var err error
		if identity, err = r.Identity(); err != nil {
			return "", err
		}
	}
	worktree, err := r.repo.Worktree()
	if err != nil {
		return "", classify(ctx, op, "", err)
	}
	worktree.Excludes = r.excludePatterns()
	status, err := worktree.Status()
	if err != nil {
		return "", classify(ctx, op, "", err)
	}
	attributes := r.worktreeAttributes(status)
	staged := 0
	for path, change := range status {
		if err = ctx.Err(); err != nil {
			return "", newError(KindCanceled, op, "", err)
		}
		if !selectedPath(path, options.Paths) || (change.Worktree == git.Unmodified && change.Staging == git.Unmodified) {
			continue
		}
		if change.Staging == git.Deleted && change.Worktree == git.Unmodified {
			staged++ // Already removed from the index.
			continue
		}
		if change.Worktree == git.Deleted {
			_, err = worktree.Remove(path)
		} else {
			var previous plumbing.Hash
			if entry, known := r.indexEntry(path); known {
				previous = entry
			}
			if _, err = worktree.Add(path); err == nil {
				err = r.stageNormalized(ctx, op, path, previous, attributes)
			}
		}
		if err != nil {
			return "", classify(ctx, op, path, err)
		}
		staged++
	}
	if staged == 0 {
		return "", newError(KindNoChanges, op, "", nil)
	}
	signature := &object.Signature{Name: identity.Name, Email: identity.Email, When: time.Now()}
	hash, err := worktree.Commit(options.Message, &git.CommitOptions{Author: signature, Committer: signature})
	if err != nil {
		return "", classify(ctx, op, "", err)
	}
	return hash.String(), nil
}

func selectedPath(path string, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, candidate := range paths {
		candidate = strings.TrimSuffix(candidate, "/")
		if path == candidate || strings.HasPrefix(path, candidate+"/") {
			return true
		}
	}
	return false
}

func (r *Repo) pushAuth(ctx context.Context, op string) (target, *git.Remote, error) {
	remote, err := r.origin()
	if err != nil {
		return target{}, nil, err
	}
	handle, err := r.repo.Remote(originName)
	if err != nil {
		return target{}, nil, classify(ctx, op, "", err)
	}
	return remote, handle, nil
}

// Push publishes commit to branch without force. Credentials are required for
// https remotes.
func (r *Repo) Push(ctx context.Context, commit, branch string) error {
	const op = "push"
	if err := r.validatePush(op, commit, branch); err != nil {
		return err
	}
	remote, handle, err := r.pushAuth(ctx, op)
	if err != nil {
		return err
	}
	auth, err := r.client.auth(ctx, op, remote, true)
	if err != nil {
		return err
	}
	spec := config.RefSpec(commit + ":" + plumbing.NewBranchReferenceName(branch).String())
	err = handle.PushContext(ctx, &git.PushOptions{RemoteURL: remote.url, Auth: auth, RemoteName: originName, RefSpecs: []config.RefSpec{spec}})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return classify(ctx, op, branch, err)
}

func (r *Repo) validatePush(op, commit, branch string) error {
	if err := checkBranch(op, branch); err != nil {
		return err
	}
	if !sha1Pattern.MatchString(commit) {
		return invalid(op, "not a full commit hash: %q", commit)
	}
	return nil
}

// PushLease force-updates branch to commit only if the remote branch is still at
// expected, like git push --force-with-lease=<ref>:<expected>. A stale lease
// yields KindLeaseConflict and leaves the remote unchanged.
//
// go-git ignores leases on SHA refspecs and checks them only through a named
// local ref with a tracking ref, so both are written for the push and restored
// afterwards.
func (r *Repo) PushLease(ctx context.Context, commit, branch, expected string) error {
	const op = "push with lease"
	if err := r.validatePush(op, commit, branch); err != nil {
		return err
	}
	if !sha1Pattern.MatchString(expected) {
		return invalid(op, "not a full commit hash: %q", expected)
	}
	remote, handle, err := r.pushAuth(ctx, op)
	if err != nil {
		return err
	}
	auth, err := r.client.auth(ctx, op, remote, true)
	if err != nil {
		return err
	}
	local := plumbing.NewBranchReferenceName(branch)
	tracking := plumbing.NewRemoteReferenceName(originName, branch)
	restore, err := r.withLeaseRefs(local, tracking, plumbing.NewHash(commit), plumbing.NewHash(expected))
	if err != nil {
		return classify(ctx, op, branch, err)
	}
	defer restore()
	err = handle.PushContext(ctx, &git.PushOptions{
		RemoteURL:      remote.url,
		Auth:           auth,
		RemoteName:     originName,
		RefSpecs:       []config.RefSpec{config.RefSpec(fmt.Sprintf("+%s:%s", local, local))},
		ForceWithLease: &git.ForceWithLease{RefName: local, Hash: plumbing.NewHash(expected)},
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	if ctx.Err() == nil && isLeaseRejection(err) {
		return newError(KindLeaseConflict, op, branch, err)
	}
	return classify(ctx, op, branch, err)
}

// withLeaseRefs points the two refs go-git needs at the lease and returns a
// function restoring their previous values.
func (r *Repo) withLeaseRefs(local, tracking plumbing.ReferenceName, commit, expected plumbing.Hash) (func(), error) {
	saved := map[plumbing.ReferenceName]*plumbing.Reference{}
	for _, name := range []plumbing.ReferenceName{local, tracking} {
		existing, err := r.repo.Storer.Reference(name)
		if err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, err
		}
		saved[name] = existing
	}
	restore := func() {
		for name, previous := range saved {
			if previous == nil {
				_ = r.repo.Storer.RemoveReference(name)
			} else {
				_ = r.repo.Storer.SetReference(previous)
			}
		}
	}
	for name, hash := range map[plumbing.ReferenceName]plumbing.Hash{local: commit, tracking: expected} {
		if err := r.repo.Storer.SetReference(plumbing.NewHashReference(name, hash)); err != nil {
			restore()
			return nil, err
		}
	}
	return restore, nil
}
