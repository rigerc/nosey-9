package gitpolicy

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialedEnvKeepsUserConfigurationButRestrictsTransport(t *testing.T) {
	env := CredentialedEnv()
	require.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
	require.Contains(t, env, "GIT_ALLOW_PROTOCOL=https:ssh")
	require.Contains(t, env, "GIT_LFS_SKIP_SMUDGE=1")
	for _, e := range env {
		require.False(t, strings.HasPrefix(e, "GIT_CONFIG_GLOBAL="), "user configuration must stay visible")
		require.False(t, strings.HasPrefix(e, "GIT_CONFIG_NOSYSTEM="))
	}
}

func TestHardenedEnvStillNullsConfiguration(t *testing.T) {
	require.Contains(t, HardenedEnv(), "GIT_CONFIG_GLOBAL="+os.DevNull)
	require.Contains(t, HardenedEnv(), "GIT_CONFIG_NOSYSTEM=1")
}

func TestCredentialedArgsDisableHooksExtAndSubmodules(t *testing.T) {
	args := CredentialedArgs("fetch", "origin")
	require.Equal(t, []string{"fetch", "origin"}, args[len(args)-2:])
	for _, want := range []string{"core.hooksPath=" + os.DevNull, "protocol.ext.allow=never", "submodule.recurse=false", "gc.auto=0"} {
		require.Contains(t, args, want)
	}
}
