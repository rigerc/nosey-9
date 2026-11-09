package skillkit

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// FolderHash returns the hash skills@1.7.6 stores as computedHash for a skill
// directory: SHA-256 over, for each regular file, its slash-separated relative
// path followed by its content. Symlinks and special files are ignored, .git and
// node_modules directories are skipped, and files are ordered by relative path
// with a locale-aware comparison matching JavaScript's String.localeCompare
// (see collatePaths). The root may itself be a symlink to a directory.
func FolderHash(dir string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	var rels []string
	if err := collectHashFiles(dir, "", &rels); err != nil {
		return "", err
	}
	collatePaths(rels)
	h := sha256.New()
	for _, rel := range rels {
		_, _ = io.WriteString(h, rel)
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func collectHashFiles(base, rel string, out *[]string) error {
	entries, err := os.ReadDir(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	for _, e := range entries {
		child := e.Name()
		if rel != "" {
			child = rel + "/" + e.Name()
		}
		switch {
		case e.IsDir():
			if e.Name() == ".git" || e.Name() == "node_modules" {
				continue
			}
			if err = collectHashFiles(base, child, out); err != nil {
				return err
			}
		case e.Type().IsRegular():
			*out = append(*out, child)
		}
	}
	return nil
}

// collatePaths sorts paths the way upstream's a.localeCompare(b) does in an
// English/root locale: a full Unicode collation with punctuation significant
// (not ignorable) and canonically equivalent strings treated as equal. Ties,
// which only occur for canonically equivalent names, break bytewise so the
// result is deterministic.
func collatePaths(paths []string) {
	c := collate.New(language.Und)
	sortStable(paths, func(a, b string) int {
		if r := c.CompareString(a, b); r != 0 {
			return r
		}
		return strings.Compare(a, b)
	})
}
