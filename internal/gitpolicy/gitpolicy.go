// Package gitpolicy validates Git URI and ref forms before argv construction.
package gitpolicy

import (
	"fmt"
	"os"
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

// HardenedEnv isolates a Git subprocess from user configuration and prompts.
func HardenedEnv() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_LFS_SKIP_SMUDGE=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
	}
}

// HardenedArgs prefixes Git arguments with settings that disable hooks and
// helper transports in repositories the caller does not trust.
func HardenedArgs(args ...string) []string {
	return append([]string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "protocol.ext.allow=never",
		"-c", "submodule.recurse=false",
	}, args...)
}
