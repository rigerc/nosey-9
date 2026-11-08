package installation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rigerc/nosey-9/internal/storage"
)

// gitSource resolves ref to a commit SHA without checking out or hashing the repository contents.
func (s Service) gitSource(ctx context.Context, uri, ref string) (string, error) {
	key := storage.Hash([]byte(uri + "#" + ref))
	dir := filepath.Join(s.Store.Cache, "skill-sources", key)
	lock, err := storage.Acquire(ctx, dir+".lock")
	if err != nil {
		return "", err
	}
	defer lock.Close()
	root := filepath.Join(dir, "checkout")
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return "", err
	}
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:ssh", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_LFS_SKIP_SMUDGE=1"}
	remote := uri
	if !strings.Contains(remote, ":") && !strings.HasPrefix(remote, "git@") {
		remote = "https://github.com/" + remote
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, os.ErrNotExist) {
		_, err = s.run(ctx, "", env, "git", "-c", "core.hooksPath="+os.DevNull, "clone", "--no-checkout", "--depth=1", "--filter=blob:none", remote, root)
		if err != nil {
			return "", fmt.Errorf("skill source unavailable: %w", err)
		}
	}
	if ref == "" {
		ref = "HEAD"
	}
	_, err = s.run(ctx, root, env, "git", "-c", "core.hooksPath="+os.DevNull, "fetch", "--depth=1", "origin", ref)
	if err != nil {
		return "", err
	}
	out, err := s.run(ctx, root, env, "git", "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out.Stdout))
	if !resolvedSHA.MatchString(sha) {
		return "", fmt.Errorf("invalid resolved Git revision")
	}
	return sha, nil
}

func treeHash(root string) (string, error) {
	var b strings.Builder
	count := 0
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source symlinks are not supported")
		}
		count++
		if count > 20000 {
			return fmt.Errorf("source file-count limit exceeded")
		}
		data, err := storage.Read(root, rel, 16<<20)
		if err != nil {
			return err
		}
		total += int64(len(data))
		if total > 64<<20 {
			return fmt.Errorf("source byte limit exceeded")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s\x00%d\x00%s\n", filepath.ToSlash(rel), info.Mode().Perm()&0o111, storage.Hash(data))
		return nil
	})
	return storage.Hash([]byte(b.String())), err
}

func copyTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".install-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	err = filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(stage, rel), 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe source symlink")
		}
		data, err := storage.Read(src, rel, 16<<20)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(stage, rel), data, 0o600|info.Mode().Perm()&0o111)
	})
	if err != nil {
		return err
	}
	return os.Rename(stage, dst)
}
