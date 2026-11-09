package skillkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/storage"
)

// LockFile is the project lock file name shared with `npx skills`.
const LockFile = "skills-lock.json"

// LockVersion is the only lock format version this package reads and writes.
const LockVersion = 1

const lockReadLimit = 16 << 20

// LockError describes why skills-lock.json cannot be used; Reason is safe to show to users.
type LockError struct{ Reason string }

func (e LockError) Error() string { return LockFile + " " + e.Reason }

// ErrLockMissing is returned by ReadLock when no lock file exists.
var ErrLockMissing = LockError{"is missing"}

// LockEntry is one skill in skills-lock.json. Fields Nosey does not know are
// kept in Extra and written back unchanged.
type LockEntry struct {
	// Source is the logical source: owner/repo, a git URL, or a local path (absolute in memory).
	Source string
	// SourceURL is the clone URL for git and gitlab sources.
	SourceURL string
	// Ref is the requested ref (branch, tag or SHA), never the resolved commit.
	Ref string
	// SourceType is github, gitlab, git, local, node_modules, well-known, ...
	SourceType string
	// SkillPath is the SKILL.md path inside the source repository.
	SkillPath string
	// ComputedHash is FolderHash of the skill as installed by skills@1.7.6 would record.
	ComputedHash string
	// Version is the package version recorded for node_modules sources.
	Version string
	// Extra holds unknown fields.
	Extra map[string]json.RawMessage

	order []string
}

// ProjectLock is the parsed skills-lock.json.
type ProjectLock struct {
	Version int
	Skills  map[string]LockEntry
	// Extra holds unknown top-level fields.
	Extra map[string]json.RawMessage

	order []string
}

// NewProjectLock returns an empty lock.
func NewProjectLock() ProjectLock {
	return ProjectLock{Version: LockVersion, Skills: map[string]LockEntry{}}
}

var knownEntryFields = []string{"source", "sourceUrl", "ref", "sourceType", "skillPath", "version", "computedHash"}

// ReadLock reads and strictly validates workspace/skills-lock.json. A missing
// file yields ErrLockMissing; every other problem is a LockError.
func ReadLock(workspace string) (ProjectLock, error) {
	data, err := storage.Read(workspace, LockFile, lockReadLimit)
	if errors.Is(err, os.ErrNotExist) {
		return ProjectLock{}, ErrLockMissing
	}
	if err != nil {
		return ProjectLock{}, LockError{"cannot be read: " + err.Error()}
	}
	return ParseLock(data, workspace)
}

// ParseLock validates lock content. workspace resolves relative local sources.
func ParseLock(data []byte, workspace string) (ProjectLock, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		reason := "is not valid JSON"
		if bytes.Contains(data, []byte("<<<<<<<")) {
			reason += " (unresolved merge conflict markers)"
		}
		return ProjectLock{}, LockError{reason}
	}
	var version int
	if err := json.Unmarshal(top["version"], &version); err != nil && top["version"] != nil {
		return ProjectLock{}, LockError{"is not valid JSON"}
	}
	if version != LockVersion {
		return ProjectLock{}, LockError{fmt.Sprintf("has unsupported version %d (expected %d)", version, LockVersion)}
	}
	rawSkills := top["skills"]
	var skills map[string]json.RawMessage
	if err := json.Unmarshal(rawSkills, &skills); err != nil && rawSkills != nil && string(rawSkills) != "null" {
		return ProjectLock{}, LockError{"is not valid JSON"}
	}
	if skills == nil {
		return ProjectLock{}, LockError{`has no "skills" map`}
	}
	lock := ProjectLock{Version: version, Skills: make(map[string]LockEntry, len(skills)), order: keyOrder(data)}
	for k, v := range top {
		if k != "version" && k != "skills" {
			if lock.Extra == nil {
				lock.Extra = map[string]json.RawMessage{}
			}
			lock.Extra[k] = v
		}
	}
	for name, raw := range skills {
		entry, err := parseEntry(raw)
		if err != nil {
			return ProjectLock{}, LockError{fmt.Sprintf("has an invalid entry for %q: %v", name, err)}
		}
		if entry.SourceType == "local" && entry.Source != "" && !filepath.IsAbs(entry.Source) {
			entry.Source = filepath.Join(workspace, filepath.FromSlash(entry.Source))
		}
		lock.Skills[name] = entry
	}
	return lock, nil
}

func parseEntry(raw json.RawMessage) (LockEntry, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return LockEntry{}, errors.New("expected an object")
	}
	e := LockEntry{order: keyOrder(raw)}
	targets := map[string]*string{
		"source": &e.Source, "sourceUrl": &e.SourceURL, "ref": &e.Ref, "sourceType": &e.SourceType,
		"skillPath": &e.SkillPath, "computedHash": &e.ComputedHash, "version": &e.Version,
	}
	for k, v := range fields {
		target, known := targets[k]
		if !known {
			if e.Extra == nil {
				e.Extra = map[string]json.RawMessage{}
			}
			e.Extra[k] = v
			continue
		}
		if string(v) == "null" {
			continue
		}
		if err := json.Unmarshal(v, target); err != nil {
			return LockEntry{}, fmt.Errorf("field %q must be a string", k)
		}
	}
	return e, nil
}

// keyOrder returns the member names of the top-level JSON object in document order.
func keyOrder(data []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		key, _ := t.(string)
		keys = append(keys, key)
		var skip json.RawMessage
		if err = dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

// Set adds or replaces a skill. Unknown fields of an existing entry are kept
// unless entry supplies the same key.
func (l *ProjectLock) Set(name string, entry LockEntry) {
	if l.Skills == nil {
		l.Skills = map[string]LockEntry{}
	}
	if old, ok := l.Skills[name]; ok {
		if len(entry.order) == 0 {
			entry.order = old.order
		}
		for k, v := range old.Extra {
			if _, set := entry.Extra[k]; !set {
				if entry.Extra == nil {
					entry.Extra = map[string]json.RawMessage{}
				}
				entry.Extra[k] = v
			}
		}
	}
	l.Skills[name] = entry
}

// Remove deletes a skill.
func (l *ProjectLock) Remove(name string) { delete(l.Skills, name) }

// Names lists skill names in sorted order.
func (l ProjectLock) Names() []string {
	names := make([]string, 0, len(l.Skills))
	for n := range l.Skills {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Marshal renders the lock as skills@1.7.6 writes it: two-space indentation,
// skills sorted by name, a trailing newline, and local sources relative to
// workspace. Key order of entries read from disk is preserved.
func (l ProjectLock) Marshal(workspace string) ([]byte, error) {
	skills := map[string]json.RawMessage{}
	for name, e := range l.Skills {
		if e.SourceType == "local" {
			e.Source = portableLocalSource(e.Source, workspace)
		}
		raw, err := marshalEntry(e)
		if err != nil {
			return nil, err
		}
		skills[name] = raw
	}
	names := make([]string, 0, len(skills))
	for n := range skills {
		names = append(names, n)
	}
	slices.Sort(names)
	var skillsBuf bytes.Buffer
	if len(names) == 0 {
		skillsBuf.WriteString("{}")
	} else {
		skillsBuf.WriteString("{\n")
		for i, n := range names {
			k, _ := marshalString(n)
			skillsBuf.WriteString("    " + string(k) + ": ")
			var ind bytes.Buffer
			if err := json.Indent(&ind, skills[n], "    ", "  "); err != nil {
				return nil, err
			}
			skillsBuf.Write(ind.Bytes())
			if i < len(names)-1 {
				skillsBuf.WriteByte(',')
			}
			skillsBuf.WriteByte('\n')
		}
		skillsBuf.WriteString("  }")
	}
	version, _ := json.Marshal(l.Version)
	top := map[string]json.RawMessage{"version": version, "skills": skillsBuf.Bytes()}
	for k, v := range l.Extra {
		top[k] = v
	}
	out, err := writeObject(top, l.order, []string{"version", "skills"}, false)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func marshalEntry(e LockEntry) ([]byte, error) {
	vals := map[string]json.RawMessage{}
	for k, v := range e.Extra {
		vals[k] = v
	}
	for k, v := range map[string]string{
		"source": e.Source, "sourceUrl": e.SourceURL, "ref": e.Ref, "sourceType": e.SourceType,
		"skillPath": e.SkillPath, "version": e.Version, "computedHash": e.ComputedHash,
	} {
		if v != "" || k == "source" || k == "sourceType" || k == "computedHash" && slices.Contains(e.order, k) {
			s, err := marshalString(v)
			if err != nil {
				return nil, err
			}
			vals[k] = s
		}
	}
	return writeObject(vals, e.order, knownEntryFields, true)
}

// writeObject emits an object with members in `order` (those present), then the
// remaining canonical members, then the rest sorted. When compact it writes one line;
// callers re-indent. Unindented raw members are re-indented by json.Indent.
func writeObject(vals map[string]json.RawMessage, order, canonical []string, compact bool) ([]byte, error) {
	var keys []string
	seen := map[string]bool{}
	for _, k := range append(slices.Clone(order), canonical...) {
		if _, ok := vals[k]; ok && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range vals {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	keys = append(keys, rest...)
	var buf bytes.Buffer
	if compact {
		buf.WriteByte('{')
		for i, k := range keys {
			ks, _ := marshalString(k)
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(ks)
			buf.WriteByte(':')
			var c bytes.Buffer
			if err := json.Compact(&c, vals[k]); err != nil {
				return nil, err
			}
			buf.Write(c.Bytes())
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	}
	buf.WriteString("{\n")
	for i, k := range keys {
		ks, _ := marshalString(k)
		buf.WriteString("  " + string(ks) + ": ")
		var ind bytes.Buffer
		if err := json.Indent(&ind, vals[k], "  ", "  "); err != nil {
			return nil, err
		}
		buf.Write(ind.Bytes())
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// marshalString encodes a JSON string without HTML escaping, like JSON.stringify.
func marshalString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// portableLocalSource ports getPortableLocalSource from skills@1.7.6.
func portableLocalSource(source, lockDir string) string {
	abs := source
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(lockDir, abs)
	}
	rel, err := filepath.Rel(lockDir, abs)
	if err != nil || filepath.IsAbs(rel) {
		return filepath.ToSlash(abs)
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == "" || rel == ".":
		return "."
	case rel == ".." || strings.HasPrefix(rel, "../"):
		return rel
	}
	return "./" + rel
}

// Write atomically replaces workspace/skills-lock.json through internal/storage.
// Callers should hold the operation lock from read through write.
func (l ProjectLock) Write(workspace string) error {
	data, err := l.Marshal(workspace)
	if err != nil {
		return err
	}
	if err = storage.WriteAt(workspace, LockFile, data); err != nil {
		return err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	// #nosec G302 -- storage writes private files, but this lock is shared project state.
	return root.Chmod(LockFile, 0o644)
}

// SkillMDPath is the SKILL.md path of s relative to its source repository,
// the value stored in a lock entry's skillPath.
func SkillMDPath(s Skill) string { return path.Join(s.Path, "SKILL.md") }
