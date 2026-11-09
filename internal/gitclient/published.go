package gitclient

import (
	"context"
	"errors"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// RecordPublished writes origin's tracking ref for branch at commit. go-git's push
// leaves it unset, which Git would report as a missing upstream.
func (r *Repo) RecordPublished(commit, branch string) error {
	const op = "record published commit"
	if err := r.validatePush(op, commit, branch); err != nil {
		return err
	}
	name := plumbing.NewRemoteReferenceName(originName, branch)
	return classify(context.Background(), op, branch, r.repo.Storer.SetReference(plumbing.NewHashReference(name, plumbing.NewHash(commit))))
}

// AdoptCommit moves the checked-out branch to commit, which must already be
// present and have the same tree as HEAD, so no file changes. It replaces a local
// commit with an identical one already published.
func (r *Repo) AdoptCommit(ctx context.Context, commit string) error {
	const op = "adopt commit"
	if !sha1Pattern.MatchString(commit) {
		return invalid(op, "not a full commit hash: %q", commit)
	}
	head, err := r.Head()
	if err != nil {
		return err
	}
	if head == "" {
		return newError(KindMissingRef, op, "HEAD", errors.New("branch has no commit"))
	}
	current, err := r.TreeHash(ctx, head, "")
	if err != nil {
		return err
	}
	adopted, err := r.TreeHash(ctx, commit, "")
	if err != nil {
		return err
	}
	if current != adopted {
		return invalid(op, "commit %s has different content from HEAD", commit)
	}
	worktree, err := r.repo.Worktree()
	if err != nil {
		return classify(ctx, op, commit, err)
	}
	return classify(ctx, op, commit, worktree.Reset(&git.ResetOptions{Mode: git.MixedReset, Commit: plumbing.NewHash(commit)}))
}
