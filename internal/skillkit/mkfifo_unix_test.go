//go:build unix

package skillkit

import "syscall"

func osMkfifo(path string) error { return syscall.Mkfifo(path, 0o600) }
