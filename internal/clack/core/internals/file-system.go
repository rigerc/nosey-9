// Package internals provides OS-backed file system access for prompts.
package internals

import "os"

// OSFileSystem implements FileSystem with the host operating system.
type OSFileSystem struct{}

// Getwd returns the current working directory.
func (fs OSFileSystem) Getwd() (string, error) {
	return os.Getwd()
}

// ReadDir reads the named directory.
func (fs OSFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

// UserHomeDir returns the current user's home directory.
func (fs OSFileSystem) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}
