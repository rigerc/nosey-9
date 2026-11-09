// Package gitpolicy validates Git URI and ref forms before any network use.
package gitpolicy

import (
	"fmt"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var gitURI = regexp.MustCompile(`^(https://[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+|ssh://([A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+|git@[A-Za-z0-9.-]+:[A-Za-z0-9._/-]+|[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+)$`)

// Validate rejects unsupported URI and ref forms before any network use.
func Validate(uri, ref string) error {
	if err := ValidateURI(uri); err != nil {
		return err
	}
	return ValidateRef(ref)
}

// ValidateURI rejects unsupported Git URI forms. Accepted forms are https://,
// ssh://, scp-style git@host:path and owner/repository shorthand.
func ValidateURI(uri string) error {
	if !gitURI.MatchString(uri) || strings.Contains(uri, "..") || strings.HasPrefix(uri, "-") {
		return fmt.Errorf("unsafe Git URI")
	}
	return nil
}

// ValidateRef rejects unsafe ref forms. An empty ref means the remote default.
func ValidateRef(ref string) error {
	if ref != "" && (!refPattern.MatchString(ref) || strings.Contains(ref, "..") || strings.HasSuffix(ref, "/") || strings.Contains(ref, "//")) {
		return fmt.Errorf("unsafe Git ref")
	}
	return nil
}
