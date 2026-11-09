package skillkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BusyError reports that another Nosey operation holds the workspace lock.
// It is distinct from context cancellation and from I/O errors.
type BusyError struct {
	Workspace string
	Waited    time.Duration
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("another Nosey operation is running in %s (waited %s)", e.Workspace, e.Waited.Round(time.Millisecond))
}

// LockOptions tunes acquisition.
type LockOptions struct {
	// Timeout bounds how long to wait for a busy lock; zero means 10 seconds.
	Timeout time.Duration
	// Poll is the retry interval; zero means 50 milliseconds.
	Poll time.Duration
}

// OperationLock is a held workspace lock. The kernel releases the underlying
// advisory lock if the process dies, so a crash never leaves a workspace locked.
type OperationLock struct {
	file *os.File
	path string
}

// Path is the lock file location (inside the data directory, never the workspace).
func (l *OperationLock) Path() string { return l.path }

// Release drops the lock. It is safe to call more than once.
func (l *OperationLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	err := unlockFile(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// LockPath returns the lock file for a workspace: dataDir/locks/<sha256 of the
// canonical workspace path>.lock. The lock lives in the data directory because
// a project must not gain a directory from an install.
func LockPath(dataDir, workspace string) (string, error) {
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", err
	}
	if canonical, err = filepath.Abs(canonical); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(dataDir, "locks", hex.EncodeToString(sum[:16])+".lock"), nil
}

// AcquireOperationLock takes the exclusive lock for workspace, waiting up to
// opts.Timeout. It returns a *BusyError if the lock stays held, and the
// context's error if ctx ends first.
func AcquireOperationLock(ctx context.Context, dataDir, workspace string, opts LockOptions) (*OperationLock, error) {
	if dataDir == "" {
		return nil, errors.New("data directory is required for the operation lock")
	}
	timeout, poll := opts.Timeout, opts.Poll
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	lockPath, err := LockPath(dataDir, workspace)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	// #nosec G304 -- application-owned path derived from the data directory and a hash.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		locked, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if locked {
			_ = f.Truncate(0)
			_, _ = fmt.Fprintf(f, "pid %d\n", os.Getpid())
			return &OperationLock{file: f, path: lockPath}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = f.Close()
			return nil, &BusyError{Workspace: workspace, Waited: time.Since(start)}
		case <-time.After(poll):
		}
	}
}
