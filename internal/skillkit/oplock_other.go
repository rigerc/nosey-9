//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package skillkit

import (
	"errors"
	"os"
)

// Platforms without flock or LockFileEx have no kernel-released lock here.
// Acquiring fails explicitly instead of pretending to exclude other processes.
var errNoKernelLock = errors.New("operation locking is not supported on this platform")

func tryLockFile(*os.File) (bool, error) { return false, errNoKernelLock }
func unlockFile(*os.File) error          { return nil }
