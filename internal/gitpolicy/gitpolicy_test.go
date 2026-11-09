package gitpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAcceptsSupportedForms(t *testing.T) {
	for _, uri := range []string{"https://github.com/o/r.git", "ssh://git@host/o/r", "git@host:o/r.git", "o/r"} {
		require.NoError(t, Validate(uri, "v1.2"), uri)
	}
}

func TestValidateRejectsUnsafeForms(t *testing.T) {
	for _, uri := range []string{"file:///tmp/x", "http://host/o/r", "-o/r", "https://host/o/../r", "ext::sh -c x"} {
		require.Error(t, Validate(uri, ""), uri)
	}
	for _, ref := range []string{"-x", "a..b", "a//b", "a/"} {
		require.Error(t, Validate("o/r", ref), ref)
	}
}
