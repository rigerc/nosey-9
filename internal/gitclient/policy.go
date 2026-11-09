package gitclient

import (
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
)

const (
	schemeHTTPS = "https"
	schemeHTTP  = "http"
	schemeSSH   = "ssh"
	schemeFile  = "file"
)

// Policy is the per-caller transport policy, enforced before any connection.
//
// Git's url.insteadOf is deliberately not honoured: a URL means what it says.
type Policy struct {
	// AllowHTTP also permits plain http remotes.
	AllowHTTP bool
	// TrustedOrigin replaces the strict URI grammar with structural checks, for
	// the origin of the user's own repository, which may carry a port. It does not
	// widen which hosts receive a token.
	TrustedOrigin bool

	// AllowFile permits file:// and absolute path remotes. Test seam: only
	// _test.go files may set it (TestFileSeamOnlySetInTests enforces this).
	AllowFile bool
	// Rewrite maps a validated URL to the URL actually dialled. Test seam with the
	// same restriction as AllowFile.
	Rewrite func(string) string
}

// Hardened permits https and ssh only, for sources Nosey does not control.
func Hardened() Policy { return Policy{} }

// UserOrigin is for a repository the user owns: http is also permitted.
func UserOrigin() Policy { return Policy{AllowHTTP: true, TrustedOrigin: true} }

// target is a validated remote.
type target struct {
	// declared is the URL before the Rewrite seam; it is what config stores.
	declared string
	url      string
	endpoint *transport.Endpoint
	// username and password came embedded in a user origin URL. They are separated
	// from url and declared so they cannot reach logs, errors or stored config.
	username string
	password string
}

// host returns the lower-cased hostname without port.
func (t target) host() string { return strings.ToLower(t.endpoint.Host) }

var (
	shorthand = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$`)
	// helperTransport matches Git's <transport>::<address> syntax, which go-git
	// would otherwise parse as an scp-style host.
	helperTransport = regexp.MustCompile(`^[A-Za-z0-9+.-]+::`)
)

func (p Policy) resolve(raw string) (target, error) {
	const op = "validate remote"
	if raw == "" || strings.HasPrefix(raw, "-") || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return target{}, invalid(op, "unsafe Git URI")
	}
	if helperTransport.MatchString(raw) {
		return target{}, invalid(op, "transport helpers are not permitted")
	}
	normalized := raw
	switch {
	case p.AllowFile && (strings.HasPrefix(raw, "file://") || filepath.IsAbs(raw)):
	case p.TrustedOrigin:
	default:
		if err := gitpolicy.ValidateURI(raw); err != nil {
			return target{}, newError(KindInvalidSource, op, "", err)
		}
		if shorthand.MatchString(raw) {
			normalized = "https://github.com/" + raw
		}
	}
	username, password := "", ""
	if p.TrustedOrigin && hasHTTPCredentials(normalized) {
		parsed, err := url.Parse(normalized)
		if err != nil {
			// The parser's message quotes the URL, credentials included.
			return target{}, invalid(op, "malformed remote URL")
		}
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
		normalized = stripCredentials(normalized)
	}
	declared := normalized
	if p.Rewrite != nil {
		normalized = p.Rewrite(normalized)
	}
	endpoint, err := transport.NewEndpoint(normalized)
	if err != nil {
		return target{}, invalid(op, "malformed remote URL %s", redactURL(normalized))
	}
	switch endpoint.Protocol {
	case schemeHTTPS, schemeSSH:
	case schemeHTTP:
		if !p.AllowHTTP {
			return target{}, invalid(op, "http remotes are not permitted here")
		}
	case schemeFile:
		if !p.AllowFile {
			return target{}, invalid(op, "local path and file remotes are not supported")
		}
		return target{declared: declared, url: normalized, endpoint: endpoint}, nil
	default:
		return target{}, invalid(op, "transport %q is not permitted", endpoint.Protocol)
	}
	if endpoint.Host == "" {
		return target{}, invalid(op, "remote has no host")
	}
	if endpoint.Password != "" || (endpoint.User != "" && endpoint.Protocol != schemeSSH) {
		return target{}, invalid(op, "credentials embedded in the remote URL are not supported")
	}
	return target{declared: declared, url: normalized, endpoint: endpoint, username: username, password: password}, nil
}

var urlCredentials = regexp.MustCompile(`^((?:https?|ssh)://)([^/?#]*)@`)

func hasHTTPCredentials(raw string) bool {
	match := urlCredentials.FindStringSubmatch(raw)
	return match != nil && match[1] != "ssh://"
}

func stripCredentials(raw string) string {
	return urlCredentials.ReplaceAllString(raw, "${1}")
}

// redactURL removes HTTP credentials and any SSH password, keeping an SSH
// username, which is not a secret.
func redactURL(raw string) string {
	return urlCredentials.ReplaceAllStringFunc(raw, func(prefix string) string {
		match := urlCredentials.FindStringSubmatch(prefix)
		if match[1] != "ssh://" {
			return match[1]
		}
		user, _, _ := strings.Cut(match[2], ":")
		return match[1] + user + "@"
	})
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var (
	sha1Pattern   = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// checkRef validates a requested ref. A 64-hex name is rejected here because
// plumbing.NewHash would silently truncate it to a different object name.
func checkRef(op, ref string) error {
	if sha256Pattern.MatchString(ref) {
		return newError(KindUnsupportedFormat, op, ref, nil)
	}
	if err := gitpolicy.ValidateRef(ref); err != nil {
		return newError(KindInvalidSource, op, ref, err)
	}
	return nil
}

func checkBranch(op, branch string) error {
	if branch == "" || sha1Pattern.MatchString(branch) || sha256Pattern.MatchString(branch) {
		return invalid(op, "invalid branch name %q", branch)
	}
	if err := gitpolicy.ValidateRef(branch); err != nil {
		return newError(KindInvalidSource, op, branch, err)
	}
	return nil
}
