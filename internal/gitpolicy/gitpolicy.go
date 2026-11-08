// Package gitpolicy validates Git URI and ref forms before argv construction.
package gitpolicy

import (
	"fmt"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var gitURI = regexp.MustCompile(`^(https://[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+|ssh://([A-Za-z0-9._-]+@)?[A-Za-z0-9.-]+/[A-Za-z0-9._/-]+|git@[A-Za-z0-9.-]+:[A-Za-z0-9._/-]+|[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+)$`)

// Validate rejects unsupported URI and ref forms before any subprocess use.
func Validate(uri, ref string) error {
	if !gitURI.MatchString(uri) || strings.Contains(uri, "..") || strings.HasPrefix(uri, "-") {
		return fmt.Errorf("unsafe Git URI")
	}
	if ref != "" && (!refPattern.MatchString(ref) || strings.Contains(ref, "..") || strings.HasSuffix(ref, "/") || strings.Contains(ref, "//")) {
		return fmt.Errorf("unsafe Git ref")
	}
	return nil
}
