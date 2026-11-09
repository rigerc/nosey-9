package skillkit

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

type pluginManifestEntry struct {
	Source json.RawMessage `json:"source"` // string or object
	Skills []string        `json:"skills"`
	Name   string          `json:"name"`
}

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []pluginManifestEntry `json:"plugins"`
}

type pluginManifest struct {
	Skills []string `json:"skills"`
	Name   string   `json:"name"`
}

func isValidRelativePath(p string) bool { return strings.HasPrefix(p, "./") }

// isContainedInFS checks whether target is equal to or under base using cleaned slash paths.
func isContainedInFS(target, base string) bool {
	target, base = path.Clean(target), path.Clean(base)
	if base == "." {
		return target != ".." && !strings.HasPrefix(target, "../") && !path.IsAbs(target)
	}
	return target == base || strings.HasPrefix(target, base+"/")
}

// pluginSource reports the string source of a plugin and whether the entry is usable.
// A missing source is the plugin root; an object source is unsupported.
func pluginSource(entry pluginManifestEntry) (string, bool) {
	if len(entry.Source) == 0 || string(entry.Source) == "null" {
		return "", true
	}
	var src string
	if json.Unmarshal(entry.Source, &src) != nil {
		return "", false
	}
	return src, src == "" || isValidRelativePath(src)
}

// pluginSkillPaths lists directories declared by plugin manifests under basePath.
func pluginSkillPaths(fsys fs.FS, basePath string) []string {
	var dirs []string
	addPaths := func(pluginBase string, skills []string) {
		if !isContainedInFS(pluginBase, basePath) {
			return
		}
		for _, sp := range skills {
			if !isValidRelativePath(sp) {
				continue
			}
			skillDir := path.Dir(path.Join(pluginBase, sp))
			if isContainedInFS(skillDir, basePath) {
				dirs = append(dirs, skillDir)
			}
		}
		dirs = append(dirs, path.Join(pluginBase, "skills"))
	}
	if data, err := fs.ReadFile(fsys, path.Join(basePath, ".claude-plugin/marketplace.json")); err == nil {
		var m marketplaceManifest
		if json.Unmarshal(data, &m) == nil {
			root := m.Metadata.PluginRoot
			if root == "" || isValidRelativePath(root) {
				for _, plugin := range m.Plugins {
					if src, ok := pluginSource(plugin); ok {
						addPaths(path.Join(basePath, root, src), plugin.Skills)
					}
				}
			}
		}
	}
	if data, err := fs.ReadFile(fsys, path.Join(basePath, ".claude-plugin/plugin.json")); err == nil {
		var m pluginManifest
		if json.Unmarshal(data, &m) == nil {
			addPaths(basePath, m.Skills)
		}
	}
	return dirs
}

// pluginGroupings maps a skill directory (slash path) to the plugin that lists it.
func pluginGroupings(fsys fs.FS, basePath string) map[string]string {
	groupings := map[string]string{}
	if data, err := fs.ReadFile(fsys, path.Join(basePath, ".claude-plugin/marketplace.json")); err == nil {
		var m marketplaceManifest
		if json.Unmarshal(data, &m) == nil {
			root := m.Metadata.PluginRoot
			if root == "" || isValidRelativePath(root) {
				for _, plugin := range m.Plugins {
					src, ok := pluginSource(plugin)
					if plugin.Name == "" || !ok {
						continue
					}
					pluginBase := path.Join(basePath, root, src)
					if !isContainedInFS(pluginBase, basePath) {
						continue
					}
					for _, sp := range plugin.Skills {
						if !isValidRelativePath(sp) {
							continue
						}
						if skillDir := path.Join(pluginBase, sp); isContainedInFS(skillDir, basePath) {
							groupings[path.Clean(skillDir)] = plugin.Name
						}
					}
				}
			}
		}
	}
	if data, err := fs.ReadFile(fsys, path.Join(basePath, ".claude-plugin/plugin.json")); err == nil {
		var m pluginManifest
		if json.Unmarshal(data, &m) == nil && m.Name != "" {
			for _, sp := range m.Skills {
				if !isValidRelativePath(sp) {
					continue
				}
				if skillDir := path.Join(basePath, sp); isContainedInFS(skillDir, basePath) {
					groupings[path.Clean(skillDir)] = m.Name
				}
			}
		}
	}
	return groupings
}
