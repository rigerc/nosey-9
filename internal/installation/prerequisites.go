package installation

import (
	"context"
	"fmt"
	"strings"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

// Check records one concrete runtime compatibility probe.
type Check struct{ ID, Status, Code, Message string }

const (
	checkPass      = "pass"
	checkOK        = "OK"
	checkNoSources = "NO_CREDENTIALS"
)

// CheckPrerequisites reports the native Git and GitHub transports and which
// credential sources they would use. It names sources, never tokens or keys, and
// missing credentials pass because public repositories need none.
func (s Service) CheckPrerequisites(ctx context.Context) []Check {
	credentials := s.Credentials
	if credentials == nil {
		credentials = gitclient.SharedCredentials()
	}
	return []Check{
		{ID: "transport", Status: checkPass, Code: checkOK, Message: "native Git and GitHub clients over https and ssh; git and gh are not required"},
		githubCheck(credentials.DiagnoseGitHub(ctx)),
		sshCheck(credentials.DiagnoseSSH()),
	}
}

func githubCheck(hosts []gitclient.HostCredential) Check {
	check := Check{ID: "github-credentials", Status: checkPass, Code: checkNoSources}
	parts := make([]string, 0, len(hosts))
	for _, host := range hosts {
		source := host.Source
		if source == "" {
			source = "none"
		} else {
			check.Code = checkOK
		}
		parts = append(parts, host.Host+": "+source)
	}
	check.Message = strings.Join(parts, "; ")
	if check.Code == checkNoSources {
		check.Message += " (public repositories need no credentials)"
	}
	return check
}

func sshCheck(ssh gitclient.SSHDiagnosis) Check {
	check := Check{ID: "ssh-credentials", Status: checkPass, Code: checkNoSources}
	agent := "no SSH agent"
	if ssh.AgentReachable {
		agent = fmt.Sprintf("SSH agent with %d key(s)", ssh.AgentKeys)
	}
	files := "no unencrypted default key file"
	if len(ssh.KeyFiles) > 0 {
		files = "default key files: " + strings.Join(ssh.KeyFiles, ", ")
	}
	if ssh.AgentKeys > 0 || len(ssh.KeyFiles) > 0 {
		check.Code = checkOK
	}
	hosts := "no known_hosts file"
	if ssh.KnownHosts {
		hosts = "known_hosts present"
	}
	check.Message = agent + "; " + files + "; " + hosts
	if check.Code == checkNoSources {
		check.Message += " (only needed for ssh remotes)"
	}
	return check
}
