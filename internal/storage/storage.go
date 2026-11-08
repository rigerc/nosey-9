// Package storage supplies atomic writes and confined reads for untrusted data.
package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Write atomically replaces an application-owned data file.
func Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".nosey-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Read confines symlinks and relative paths to root and bounds memory use.
func Read(root, name string, limit int64) ([]byte, error) {
	if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
		return nil, fmt.Errorf("unsafe relative path %q", name)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("file exceeds limit or is not regular: %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("file exceeds limit: %s", name)
	}
	return data, err
}

// Hash returns lowercase SHA-256 for content identity.
func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// JSON writes typed application state with stable object key order.
// encoding/json.MarshalIndent defines the dynamic value at this serialization boundary.
func JSON[T any](path string, value T) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, append(data, '\n'))
}

// Safe removes terminal control characters from untrusted display text.
func Safe(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r >= 127 && r <= 159 {
			return -1
		}
		return r
	}, text)
}

// Lock owns an exclusive application lock until Close.
type Lock struct{ path string }

// Acquire waits for an exclusive lock with cancellation and a bounded deadline.
func Acquire(ctx context.Context, path string) (Lock, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Lock{}, err
	}
	for {
		// #nosec G304 -- application-owned lock path derived from canonical storage roots.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return Lock{path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return Lock{}, err
		}
		select {
		case <-ctx.Done():
			return Lock{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Close releases only this acquired lock.
func (l Lock) Close() { _ = os.Remove(l.path) }

// WriteAt atomically writes beneath an existing root while confining symlink traversal.
func WriteAt(root, name string, data []byte) error {
	if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
		return fmt.Errorf("unsafe write path")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	parent := filepath.Dir(name)
	if err = r.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	temporary := filepath.Join(parent, ".nosey-"+rand.Text())
	f, err := r.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = r.Remove(temporary) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return r.Rename(temporary, name)
}
