package installation

import (
	"path/filepath"
	"strings"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

// trackedFromSource lists the tracked skills whose lock entry records spec as their source, in
// name order. spec is written as for install: a local directory (absolute or relative to the
// project), owner/repo or a Git URL, optionally with @ref; without a ref every ref matches.
func (s Service) trackedFromSource(lock skillkit.ProjectLock, spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	uri, ref := spec, ""
	if !strings.HasPrefix(spec, "git@") {
		if i := strings.LastIndex(spec, "@"); i > 0 && gitpolicy.ValidateRef(spec[i+1:]) == nil {
			uri, ref = spec[:i], spec[i+1:]
		}
	}
	local := []string{filepath.Clean(spec)}
	if !filepath.IsAbs(spec) {
		local = []string{filepath.Join(s.Store.Workspace, spec)}
		if abs, err := filepath.Abs(spec); err == nil {
			local = append(local, abs)
		}
	}
	var names []string
	for _, name := range lock.Names() {
		entry := lock.Skills[name]
		if entry.SourceType == "local" {
			source := entry.Source
			if !filepath.IsAbs(source) {
				source = filepath.Join(s.Store.Workspace, source)
			}
			for _, p := range local {
				if filepath.Clean(source) == p {
					names = append(names, name)
					break
				}
			}
			continue
		}
		switch {
		case sourceMatches(s.Store.Workspace, spec, entry.Source, entry.SourceURL):
			names = append(names, name)
		case ref != "" && strings.EqualFold(entry.Ref, ref) && sourceMatches(s.Store.Workspace, uri, entry.Source, entry.SourceURL):
			names = append(names, name)
		}
	}
	return names
}

// lockSource is the source and ref a SourcePlan fetches to refresh a tracked skill, as the
// update check fetches it: the local directory, the clone URL or the logical source.
func lockSource(entry skillkit.LockEntry) (source, ref string) {
	switch {
	case entry.SourceType == "local":
		return entry.Source, ""
	case entry.SourceURL != "":
		return entry.SourceURL, entry.Ref
	}
	return entry.Source, entry.Ref
}
