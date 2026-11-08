package detection

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/dlclark/regexp2"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

func (e *engine) match(pattern, value string, insensitive bool) (bool, error) {
	key := pattern
	if insensitive {
		key = "(?i)" + key
	}
	re, ok := e.patterns[key]
	if !ok {
		var err error
		re, err = regexp2.Compile(key, regexp2.ECMAScript)
		if err != nil {
			return false, err
		}
		re.MatchTimeout = 50 * time.Millisecond
		e.patterns[key] = re
	}
	return re.MatchString(value)
}

func (e *engine) evaluate(rule domain.Rule) ([]domain.Evidence, error) {
	found := []domain.Evidence{}
	add := func(path, identifier string) {
		project := e.projectFor(path)
		found = append(found, domain.Evidence{RuleID: rule.ID, Kind: rule.Kind, ProjectPath: project, Path: storage.Safe(path), Identifier: storage.Safe(identifier)})
	}
	if rule.Kind == "dependency" || rule.Kind == "mcp_package" {
		eco := rule.Ecosystem
		if rule.Kind == "mcp_package" {
			eco = "mcp"
		}

		for _, o := range e.observations {
			if e.projectFor(o.path) != e.scope {
				continue
			}
			if o.ecosystem != eco {
				continue
			}
			match := o.name == rule.Name
			if rule.NameRegex != "" {
				var err error
				match, err = e.match(rule.NameRegex, o.name, false)
				if err != nil {
					return found, err
				}
			}
			if match {
				add(o.path, o.name)
			}
		}
		if e.providerErrors[eco+"\x00"+e.scope] {
			return found, fmt.Errorf("unreadable %s manifest", eco)
		}
		return found, nil
	}
	if rule.Kind == "detector" {
		return e.runAdapter(rule)
	}
	for _, f := range e.files {
		if e.projectFor(f.path) != e.scope || f.directory && rule.Kind != "file" {
			continue
		}
		if e.ctx.Err() != nil {
			return found, e.ctx.Err()
		}
		name := f.path
		match := false
		switch rule.Kind {
		case "file":
			if rule.Glob != "" {
				var err error
				match, err = doublestar.PathMatch(rule.Glob, name)
				if err != nil {
					return found, err
				}
			} else {
				if rule.PathScope == "basename" {
					name = filepath.Base(name)
				}
				var err error
				match, err = e.match(rule.PathRegex, name, false)
				if err != nil {
					return found, err
				}
			}
		case "extension":
			match = strings.HasSuffix(name, rule.Extension)
		case "content":
			ok, err := doublestar.PathMatch(rule.Glob, name)
			if err != nil {
				return found, err
			}
			if !ok {
				continue
			}
			if f.err != nil {
				return found, f.err
			}
			if rule.Contains != "" {
				match = strings.Contains(string(f.data), rule.Contains)
			} else {
				match, err = e.match(rule.Regex, string(f.data), rule.RegexFlags == "i")
				if err != nil {
					return found, err
				}
			}
		case "dotenv":
			base := filepath.Base(name)
			if rule.Glob != "" {
				ok, err := doublestar.PathMatch(rule.Glob, name)
				if err != nil {
					return found, err
				}
				if !ok {
					continue
				}
			} else if !strings.HasPrefix(base, ".env") || strings.Contains(base, "example") || strings.Contains(base, "sample") {
				continue
			}
			if f.err != nil {
				return found, f.err
			}
			for _, line := range strings.Split(string(f.data), "\n") {
				key, _, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
				if !ok {
					continue
				}
				key = strings.TrimSpace(key)
				if !dotenvKey.MatchString(key) {
					continue
				}
				hit := key == rule.Key
				if rule.KeyRegex != "" {
					var err error
					hit, err = e.match(rule.KeyRegex, key, false)
					if err != nil {
						return found, err
					}
				}
				if hit {
					add(name, key)
				}
			}
			continue
		default:
			return found, fmt.Errorf("unsupported rule kind %s", rule.Kind)
		}
		if match {
			if f.unsafe {
				return found, fmt.Errorf("matched path is an unsafe symlink")
			}
			add(f.path, "")
		}
	}
	return found, nil
}

func (e *engine) add(ecosystem, name, path string) {
	if name != "" {
		e.observations = append(e.observations, observation{ecosystem, name, path})
	}
}
