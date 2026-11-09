// Package gitclient is Nosey's native Git adapter over go-git. It runs no git
// subprocess, hook, filter or credential helper, and its exported API exposes no
// go-git types.
package gitclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// Kind classifies a failure. It implements error so a caller can test
// errors.Is(err, gitclient.KindMissingRef).
type Kind string

const (
	hangupAttempts = 5
	hangupBackoff  = 20 * time.Millisecond
)

// Kinds of failure a caller can act on.
const (
	KindInvalidSource     Kind = "invalid Git source"
	KindMissingRef        Kind = "ref or commit not found"
	KindMissingPath       Kind = "path not found at commit"
	KindMissingBlob       Kind = "blob is not available in this partial fetch"
	KindMissingRepository Kind = "repository not found"
	KindAuth              Kind = "authentication failed"
	KindPermission        Kind = "permission denied"
	KindResourceLimit     Kind = "resource limit exceeded"
	KindCanceled          Kind = "operation canceled"
	KindLeaseConflict     Kind = "remote branch changed since it was last published"
	KindUnsupportedFormat Kind = "unsupported repository format"
	KindIdentityMissing   Kind = "commit identity is not configured"
	KindNoChanges         Kind = "no changes to commit"
	KindFailed            Kind = "git operation failed"
)

func (k Kind) Error() string { return string(k) }

// Error is a classified adapter failure.
type Error struct {
	Kind   Kind
	Op     string
	Remote string
	Ref    string
	Err    error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Op)
	b.WriteString(": ")
	b.WriteString(string(e.Kind))
	if e.Ref != "" {
		b.WriteString(" (" + e.Ref + ")")
	}
	if e.Err != nil {
		b.WriteString(": " + e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// Is matches on the error's Kind.
func (e *Error) Is(target error) bool {
	return target == error(e.Kind)
}

// KindOf returns err's Kind, or KindFailed when it has none.
func KindOf(err error) Kind {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Kind
	}
	return KindFailed
}

func newError(kind Kind, op, ref string, cause error) *Error {
	return &Error{Kind: kind, Op: op, Ref: ref, Err: cause}
}

func invalid(op, format string, args ...any) *Error {
	return newError(KindInvalidSource, op, "", fmt.Errorf(format, args...))
}

// classify maps a go-git failure onto a Kind. go-git reports several conditions
// only as untyped strings, so those are matched by message.
func classify(ctx context.Context, op, ref string, err error) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return err
	}
	if ctx != nil && ctx.Err() != nil {
		return newError(KindCanceled, op, ref, ctx.Err())
	}
	return newError(kindFor(err), op, ref, err)
}

func kindFor(err error) Kind {
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return KindCanceled
	case errors.Is(err, transport.ErrRepositoryNotFound), errors.Is(err, git.ErrRepositoryNotExists):
		return KindMissingRepository
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrInvalidAuthMethod):
		return KindAuth
	case errors.Is(err, transport.ErrAuthorizationFailed):
		return KindPermission
	case errors.Is(err, transport.ErrEmptyRemoteRepository),
		errors.Is(err, plumbing.ErrReferenceNotFound),
		errors.Is(err, plumbing.ErrObjectNotFound),
		errors.Is(err, git.NoMatchingRefSpecError{}),
		strings.Contains(message, "not our ref"),
		strings.Contains(message, "couldn't find remote ref"):
		return KindMissingRef
	case strings.Contains(message, "unknown extension"):
		return KindUnsupportedFormat
	case strings.Contains(message, "invalid path"):
		return KindInvalidSource
	case errors.Is(err, git.ErrEmptyCommit):
		return KindNoChanges
	case strings.Contains(message, "ssh: unable to authenticate"),
		strings.Contains(message, "knownhosts:"),
		strings.Contains(message, "known_hosts"):
		return KindAuth
	}
	return KindFailed
}

// retryOnHangup repeats op while the remote hangs up during the request. A
// streaming transport (ssh, file) can see this when the server rejects a want and
// exits while the client is still writing: the write fails with a broken pipe and
// go-git never reads the error line the server sent, so a missing commit looks like
// an unclassified failure. Which side wins depends on scheduling, so the request
// is reissued a bounded number of times to get the server's actual answer.
func retryOnHangup(ctx context.Context, op func() error) error {
	err := op()
	for attempt := 1; attempt < hangupAttempts && isRequestHangup(err); attempt++ {
		timer := time.NewTimer(hangupBackoff * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		err = op()
	}
	return err
}

func isRequestHangup(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	sending := strings.Contains(message, "sending upload-req") || strings.Contains(message, "sending haves") ||
		strings.Contains(message, "sending done") || strings.Contains(message, "closing input")
	return sending && (strings.Contains(message, "broken pipe") || strings.Contains(message, "EOF"))
}

func isLeaseRejection(err error) bool {
	return err != nil && strings.Contains(err.Error(), "non-fast-forward update")
}
