//go:build !windows

package skillkit

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOperationLockExcludesAndReleases(t *testing.T) {
	data, ws := t.TempDir(), t.TempDir()
	first, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{})
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(first.Path()))
	require.Equal(t, filepath.Join(data, "locks"), filepath.Dir(first.Path()))
	entries, err := os.ReadDir(ws)
	require.NoError(t, err)
	require.Empty(t, entries, "a workspace never gains files from locking")

	_, err = AcquireOperationLock(context.Background(), data, ws, LockOptions{Timeout: 150 * time.Millisecond, Poll: 10 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	require.Contains(t, err.Error(), "another Nosey operation is running")
	require.NotErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, context.DeadlineExceeded)

	require.NoError(t, first.Release())
	require.NoError(t, first.Release(), "Release is idempotent")
	second, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{})
	require.NoError(t, err)
	require.NoError(t, second.Release())
}

func TestOperationLockIsKeyedByCanonicalWorkspace(t *testing.T) {
	data := t.TempDir()
	ws := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(ws, link))
	a, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{})
	require.NoError(t, err)
	defer func() { _ = a.Release() }()
	_, err = AcquireOperationLock(context.Background(), data, link, LockOptions{Timeout: 100 * time.Millisecond, Poll: 10 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy, "a symlink to the workspace is the same workspace")

	other := t.TempDir()
	b, err := AcquireOperationLock(context.Background(), data, other, LockOptions{})
	require.NoError(t, err, "different workspaces do not block each other")
	require.NoError(t, b.Release())
	_, err = AcquireOperationLock(context.Background(), data, filepath.Join(ws, "missing"), LockOptions{})
	require.Error(t, err)
	_, err = AcquireOperationLock(context.Background(), "", ws, LockOptions{})
	require.Error(t, err)
}

func TestOperationLockCancelledAcquisition(t *testing.T) {
	data, ws := t.TempDir(), t.TempDir()
	held, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{})
	require.NoError(t, err)
	defer func() { _ = held.Release() }()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = AcquireOperationLock(ctx, data, ws, LockOptions{Timeout: time.Minute, Poll: 10 * time.Millisecond})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var busy *BusyError
	require.False(t, errors.As(err, &busy), "cancellation is distinct from busy")
	require.Less(t, time.Since(start), 5*time.Second)

	pre, preCancel := context.WithCancel(context.Background())
	preCancel()
	_, err = AcquireOperationLock(pre, data, ws, LockOptions{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestOperationLockWaitsForRelease(t *testing.T) {
	data, ws := t.TempDir(), t.TempDir()
	held, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{})
	require.NoError(t, err)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = held.Release()
	}()
	got, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{Timeout: 5 * time.Second, Poll: 10 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, got.Release())
}

// TestOperationLockHelperProcess is the child side of TestOperationLockReleasedWhenHolderIsKilled.
func TestOperationLockHelperProcess(t *testing.T) {
	data, ws := os.Getenv("SKILLKIT_LOCK_DATA"), os.Getenv("SKILLKIT_LOCK_WS")
	if data == "" {
		t.Skip("helper process only")
	}
	if _, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{}); err != nil {
		os.Stdout.WriteString("error: " + err.Error() + "\n")
		os.Exit(3)
	}
	os.Stdout.WriteString("locked\n")
	time.Sleep(time.Minute) // hold until killed
}

func TestOperationLockReleasedWhenHolderIsKilled(t *testing.T) {
	data, ws := t.TempDir(), t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationLockHelperProcess$")
	cmd.Env = append(os.Environ(), "SKILLKIT_LOCK_DATA="+data, "SKILLKIT_LOCK_WS="+ws)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	go func() { _ = cmd.Wait(); close(done) }()

	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)

	_, err = AcquireOperationLock(context.Background(), data, ws, LockOptions{Timeout: 150 * time.Millisecond, Poll: 10 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy, "the holding process excludes us")

	require.NoError(t, cmd.Process.Kill()) // SIGKILL: no deferred cleanup runs
	<-done

	lock, err := AcquireOperationLock(context.Background(), data, ws, LockOptions{Timeout: 5 * time.Second, Poll: 10 * time.Millisecond})
	require.NoError(t, err, "the kernel released the lock when the holder died")
	require.NoError(t, lock.Release())
}
