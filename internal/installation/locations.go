package installation

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"

	"github.com/rigerc/nosey-9/internal/skillkit"
)

const installationKey = "noseyInstallation"

// installationLocations preserves direct destinations across settings changes.
// AgentDirectories keeps legacy agent installations manageable when a direct
// destination is subsequently added for the same skill.
type installationLocations struct {
	Directories      []string `json:"directories"`
	AgentDirectories bool     `json:"agentDirectories"`
}

func recordedLocations(entry skillkit.LockEntry) (installationLocations, bool, error) {
	raw, ok := entry.Extra[installationKey]
	if !ok {
		return installationLocations{}, false, nil
	}
	var locations installationLocations
	if err := json.Unmarshal(raw, &locations); err != nil || len(locations.Directories) == 0 {
		return locations, true, PolicyError{Reason: "invalid Nosey installation locations in skills-lock.json"}
	}
	for _, dir := range locations.Directories {
		if err := skillkit.ValidateSkillsDirectory(dir); err != nil || path.Clean(dir) != dir {
			return locations, true, PolicyError{Reason: fmt.Sprintf("invalid recorded installation directory %q", dir)}
		}
	}
	return locations, true, nil
}

func (s Service) installTarget(table skillkit.AgentTable) (skillkit.Target, error) {
	target := skillkit.Target{Workspace: s.Store.Workspace, Agents: table}
	if dir := s.Settings.Installation.Directory; dir != "" {
		if err := skillkit.ValidateSkillsDirectory(dir); err != nil {
			return target, err
		}
		target.AdditionalDirs = append(target.AdditionalDirs, path.Clean(dir))
	}
	lock, err := skillkit.ReadLock(s.Store.Workspace)
	if errors.Is(err, skillkit.ErrLockMissing) {
		return target, nil
	}
	if err != nil {
		// Inventory can still show untracked files when the lock is unreadable.
		// Mutation preflight and persistence separately reject an unusable lock.
		return target, nil
	}
	for _, name := range lock.Names() {
		locations, _, err := recordedLocations(lock.Skills[name])
		if err != nil {
			return target, err
		}
		target.AdditionalDirs = append(target.AdditionalDirs, locations.Directories...)
	}
	slices.Sort(target.AdditionalDirs)
	target.AdditionalDirs = slices.Compact(target.AdditionalDirs)
	return target, nil
}

func (s Service) listInstalled(table skillkit.AgentTable) (skillkit.Inventory, error) {
	target, err := s.installTarget(table)
	if err != nil {
		return skillkit.Inventory{}, err
	}
	inventory, err := skillkit.ListInstalled(target)
	if err != nil {
		return inventory, err
	}
	lock, err := skillkit.ReadLock(s.Store.Workspace)
	if err != nil {
		return inventory, nil
	}
	return filterRecordedLocations(inventory, lock, table), nil
}

// Keep same-name files in unrelated custom directories out of a tracked skill's
// update targets. Direct-only installs must not adopt an agent's unrelated copy.
func filterRecordedLocations(inventory skillkit.Inventory, lock skillkit.ProjectLock, table skillkit.AgentTable) skillkit.Inventory {
	var skills []skillkit.InstalledSkill
	agentDirs := append(table.SkillsDirs(), skillkit.UniversalSkillsDir)
	for _, item := range inventory.Skills {
		entry, tracked := lock.Skills[item.Name]
		if !tracked {
			skills = append(skills, item)
			continue
		}
		locations, recorded, _ := recordedLocations(entry)
		var kept []skillkit.Location
		for _, location := range item.Locations {
			if slices.Contains(locations.Directories, location.Dir) || ((!recorded || locations.AgentDirectories) && slices.Contains(agentDirs, location.Dir)) {
				kept = append(kept, location)
			}
		}
		if len(kept) == 0 {
			continue
		}
		item.Locations, item.Path, item.Dir = kept, kept[0].Path, path.Base(kept[0].Path)
		item.Agents = nil
		for _, location := range kept {
			item.Agents = append(item.Agents, location.Agents...)
		}
		slices.Sort(item.Agents)
		item.Agents = slices.Compact(item.Agents)
		skills = append(skills, item)
	}
	inventory.Skills = skills
	return inventory
}

func withLocations(entry, previous skillkit.LockEntry, existed bool, dirs []string, agents bool) (skillkit.LockEntry, error) {
	locations, recorded, err := recordedLocations(previous)
	if err != nil {
		return entry, err
	}
	if !recorded {
		locations.AgentDirectories = existed
	}
	locations.AgentDirectories = locations.AgentDirectories || agents
	for _, dir := range dirs {
		locations.Directories = append(locations.Directories, path.Clean(dir))
	}
	slices.Sort(locations.Directories)
	locations.Directories = slices.Compact(locations.Directories)
	if len(locations.Directories) == 0 && !recorded {
		return entry, nil
	}
	raw, err := json.Marshal(locations)
	if err != nil {
		return entry, err
	}
	if entry.Extra == nil {
		entry.Extra = map[string]json.RawMessage{}
	}
	entry.Extra[installationKey] = raw
	return entry, nil
}
