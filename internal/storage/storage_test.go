package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicReplacementAndConfinedReads(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "state.json")
	if err := JSON(path, map[string]int{"value": 1}); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	data, err := Read(root, "nested/state.json", 64)
	if err != nil || string(data) != "replacement" {
		t.Fatal(string(data), err)
	}
	for _, name := range []string{"../escape", path, "nested\\state.json"} {
		if _, err := Read(root, name, 64); err == nil {
			t.Fatal("unsafe path read", name)
		}
	}
	if _, err := Read(root, "nested/state.json", 3); err == nil {
		t.Fatal("oversized file read")
	}
	if _, err := Read(root, "nested", 64); err == nil {
		t.Fatal("directory read as file")
	}
	if err := Write(filepath.Join(path, "invalid"), nil); err == nil {
		t.Fatal("write beneath file succeeded")
	}
	if err := JSON(path, make(chan int)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	outside := t.TempDir()
	if err := Write(filepath.Join(outside, "secret"), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")); err == nil {
		if _, err := Read(root, "link", 64); err == nil {
			t.Fatal("symlink escaped root")
		}
	}
	if Safe("\x1b[31munsafe\n\u009b") != "[31munsafe" {
		t.Fatal("terminal controls survived")
	}
	if len(Hash(data)) != 64 {
		t.Fatal("unexpected hash format")
	}
}
func TestApplicationLockCancellationAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "source.lock")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("contending lock ignored deadline", err)
	}
	first.Close()
	second, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal("released lock remained held", err)
	}
	second.Close()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatal("lock leaked", entries, err)
	}
}
