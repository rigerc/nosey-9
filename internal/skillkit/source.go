package skillkit

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
)

// Source names where skills come from. For a git source set URI (and optionally
// Ref); for a local source set LocalPath.
type Source struct {
	// URI is owner/repo, an HTTPS URL, an ssh:// URL or git@host:path.
	URI string
	// Ref is the requested branch, tag or full commit SHA; empty means the default branch.
	Ref string
	// LocalPath is a directory used as-is; no fetch happens.
	LocalPath string
}

// ParsedSource is a Source resolved the way skills@1.7.6 parses its argument.
type ParsedSource struct {
	// Type is github, gitlab, git or local.
	Type string
	// URL is the clone URL, or the absolute directory for local sources.
	URL string
	// Ref is the requested ref, unchanged.
	Ref string
	// LocalPath is set for local sources.
	LocalPath string
}

var shorthand = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)$`)

// ParseSource validates src with gitpolicy and classifies it.
func ParseSource(src Source) (ParsedSource, error) {
	if src.LocalPath != "" {
		abs, err := filepath.Abs(src.LocalPath)
		if err != nil {
			return ParsedSource{}, err
		}
		return ParsedSource{Type: "local", URL: abs, LocalPath: abs, Ref: src.Ref}, nil
	}
	if err := gitpolicy.Validate(src.URI, src.Ref); err != nil {
		return ParsedSource{}, &SourceError{URI: src.URI, Ref: src.Ref, Err: err}
	}
	uri := src.URI
	if m := shorthand.FindStringSubmatch(uri); m != nil {
		repo := strings.TrimSuffix(m[2], ".git")
		return ParsedSource{Type: "github", URL: "https://github.com/" + m[1] + "/" + repo + ".git", Ref: src.Ref}, nil
	}
	if strings.HasPrefix(uri, "https://") {
		u, err := url.Parse(uri)
		if err != nil || u.Host == "" {
			return ParsedSource{}, &SourceError{URI: src.URI, Ref: src.Ref, Err: fmt.Errorf("unsupported URI")}
		}
		repoPath := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
		switch strings.ToLower(u.Host) {
		case "github.com":
			if parts := strings.Split(repoPath, "/"); len(parts) >= 2 {
				return ParsedSource{Type: "github", URL: "https://github.com/" + parts[0] + "/" + parts[1] + ".git", Ref: src.Ref}, nil
			}
		case "gitlab.com":
			if strings.Contains(repoPath, "/") {
				return ParsedSource{Type: "gitlab", URL: "https://gitlab.com/" + repoPath + ".git", Ref: src.Ref}, nil
			}
		}
	}
	return ParsedSource{Type: "git", URL: uri, Ref: src.Ref}, nil
}

// ownerRepo ports getOwnerRepo: owner/repo for hosted git URLs, "" otherwise.
func ownerRepo(p ParsedSource) string {
	if p.Type == "local" {
		return ""
	}
	u := p.URL
	if rest, ok := strings.CutPrefix(u, "git@"); ok {
		_, repoPath, found := strings.Cut(rest, ":")
		repoPath = strings.TrimSuffix(repoPath, ".git")
		if found && strings.Contains(repoPath, "/") {
			return repoPath
		}
		return ""
	}
	if !strings.HasPrefix(u, "ssh://") && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	repoPath := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), ".git")
	if strings.Contains(repoPath, "/") {
		return repoPath
	}
	return ""
}

// LockSource ports getProjectLockSource: the lock `source` value.
func LockSource(p ParsedSource) string {
	u := p.URL
	norm := ownerRepo(p)
	switch {
	case strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "ssh://"):
		return u
	case strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://"):
		if parsed, err := url.Parse(u); err != nil {
			if norm != "" {
				return norm
			}
		} else if parsed.Hostname() != "github.com" {
			return u
		}
	}
	if norm != "" {
		return norm
	}
	return u
}

// NewLockEntry ports projectLockEntry: the entry skills@1.7.6 writes for a
// skill installed from p. skillPath is SkillMDPath for git sources and empty
// for local ones; Ref is the requested ref only, never the resolved commit.
func NewLockEntry(p ParsedSource, skillPath, computedHash string) LockEntry {
	e := LockEntry{Source: LockSource(p), Ref: p.Ref, SourceType: p.Type, SkillPath: skillPath, ComputedHash: computedHash}
	if p.Type == "git" || p.Type == "gitlab" {
		e.SourceURL = p.URL
	}
	return e
}

// SourceError reports a rejected source.
type SourceError struct {
	URI, Ref string
	Err      error
}

func (e *SourceError) Error() string { return "invalid skill source: " + e.Err.Error() }
func (e *SourceError) Unwrap() error { return e.Err }
