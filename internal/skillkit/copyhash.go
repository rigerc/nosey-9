package skillkit

import (
	"context"
	"os"
	"path/filepath"
)

// CopyHash returns the FolderHash of the tree an install of the skill at dir would
// produce: the copy rules (metadata.json and cache directories excluded, internal
// links followed, escaping links skipped) are applied before hashing. Update checks
// compare it with the FolderHash of an installed tree, because the plain FolderHash
// of a raw checkout differs from the hash of its copy whenever those rules apply.
func CopyHash(ctx context.Context, dir string) (string, error) {
	tmp, err := os.MkdirTemp("", "nosey-hash-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dest, err := openDest(tmp)
	if err != nil {
		return "", err
	}
	defer dest.Close()
	if _, err = dest.copySkill(ctx, dir, "skill"); err != nil {
		return "", err
	}
	return FolderHash(filepath.Join(dest.real, "skill"))
}
