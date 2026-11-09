package skillkit

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillMetadata holds the optional frontmatter metadata Nosey reads.
type SkillMetadata struct {
	Internal bool
}

// Skill is a SKILL.md found in a source tree.
type Skill struct {
	// Name is the frontmatter name, used for lock keys and display.
	Name string
	// Description is the frontmatter description.
	Description string
	// Metadata holds optional frontmatter metadata.
	Metadata SkillMetadata
	// Path is the slash-separated directory relative to the discovery root; "." for the root.
	Path string
	// Dir is the absolute directory holding SKILL.md.
	Dir string
	// PluginName names the plugin manifest that lists this skill, if any.
	PluginName string
}

// ParseSkill parses SKILL.md content. Name and description must be strings.
func ParseSkill(data []byte) (Skill, error) {
	frontmatter, err := splitFrontmatter(data)
	if err != nil {
		return Skill{}, fmt.Errorf("parsing frontmatter: %w", err)
	}
	var raw map[string]any
	if err = yaml.Unmarshal(frontmatter, &raw); err != nil {
		return Skill{}, fmt.Errorf("parsing skill frontmatter YAML: %w", err)
	}
	var s Skill
	s.Name, _ = raw["name"].(string)
	s.Description, _ = raw["description"].(string)
	if meta, ok := raw["metadata"].(map[string]any); ok {
		s.Metadata.Internal, _ = meta["internal"].(bool)
	}
	if s.Name == "" {
		return Skill{}, errors.New("skill name is required")
	}
	if s.Description == "" {
		return Skill{}, errors.New("skill description is required")
	}
	return s, nil
}

// splitFrontmatter returns the YAML between the first two "---" lines.
func splitFrontmatter(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty file")
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.Split(string(data), "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return nil, errors.New("file must start with ---")
	}
	var out []string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return []byte(strings.Join(out, "\n") + "\n"), nil
		}
		out = append(out, strings.TrimSuffix(line, "\r"))
	}
	return nil, errors.New("unclosed frontmatter (missing closing ---)")
}

// SanitizeName turns a skill name into a safe directory name, as skills@1.7.6 does:
// lowercase, runs of characters outside [a-z0-9._] become one hyphen, leading and
// trailing dots and hyphens are trimmed, and the result is at most 255 bytes.
func SanitizeName(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	out := strings.Trim(b.String(), ".-")
	if len(out) > 255 {
		out = out[:255]
	}
	if out == "" {
		return "unnamed-skill"
	}
	return out
}
