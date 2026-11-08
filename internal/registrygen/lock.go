package registrygen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rigerc/nosey-9/internal/storage"
)

// FileEntry records the canonical hash and optional resolved upstream revision.
type FileEntry struct {
	SHA256   string `json:"sha256"`
	Revision string `json:"revision,omitempty"`
}

// ManifestEntry records the published manifest path and hash.
type ManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Summary holds the entity counts published by this generation.
type Summary struct {
	Skills       int `json:"skills"`
	Technologies int `json:"technologies"`
	Mappings     int `json:"mappings"`
}

// Lock is the deterministic generation record written last.
type Lock struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Generator     string               `json:"generator"`
	Files         map[string]FileEntry `json:"files"`
	Manifest      ManifestEntry        `json:"manifest"`
	Summary       Summary              `json:"summary"`
}

// encode renders the lock with stable key order and a trailing newline.
// encoding/json sorts map keys, so dynamic maps are deterministic.
func (l Lock) encode() ([]byte, error) {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// readLock returns the on-disk lock and whether it exists.
func readLock(root string) (Lock, bool, error) {
	var lock Lock
	data, err := storage.Read(root, LockName, 1<<20)
	if err != nil {
		if os.IsNotExist(err) {
			return lock, false, nil
		}
		return lock, false, err
	}
	if err = json.Unmarshal(data, &lock); err != nil {
		return lock, true, inputError("%s: %w", LockName, err)
	}
	return lock, true, nil
}

// equal compares two locks by their canonical bytes.
func (l Lock) equal(other Lock) (bool, error) {
	a, err := l.encode()
	if err != nil {
		return false, err
	}
	b, err := other.encode()
	if err != nil {
		return false, err
	}
	return bytes.Equal(a, b), nil
}

// revisionFor carries a previously resolved revision forward for an upstream role.
func revisionFor(old Lock, path string) string {
	entry, ok := old.Files[path]
	if !ok {
		return ""
	}
	return entry.Revision
}

// fullRevision reports whether value is a lowercase 40-character commit SHA.
func fullRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// hashBytes is package-local for readability in generation summaries.
func hashBytes(data []byte) string { return storage.Hash(data) }

// describeLock is used in diagnostics when the computed lock differs.
func describeLock() string { return fmt.Sprintf("%s is stale", LockName) }
