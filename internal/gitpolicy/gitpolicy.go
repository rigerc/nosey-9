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

// CredentialedEnv is the environment for the one Git command that must see the
// user's global Git configuration so credential helpers, SSH settings and URL
// rewrites work for private repositories. Unlike HardenedEnv it does not null
// the user configuration, so callers must run only a network fetch with it,
// never a checkout: a fetch executes no smudge or clean filters. It still
// disables terminal prompts and LFS smudge and restricts transports to HTTPS
// and SSH. Pair it with CredentialedArgs.
func CredentialedEnv() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=https:ssh",
		"GIT_LFS_SKIP_SMUDGE=1",
	}
}

// CredentialedArgs prefixes Git arguments for the credentialed fetch. It applies
// HardenedArgs, so command-line settings override hooks, the ext transport and
// submodule recursion even if the user configuration enables them, and turns off
// automatic maintenance that could otherwise run after the fetch.
func CredentialedArgs(args ...string) []string {
	return HardenedArgs(append([]string{"-c", "gc.auto=0", "-c", "maintenance.auto=false"}, args...)...)
}
