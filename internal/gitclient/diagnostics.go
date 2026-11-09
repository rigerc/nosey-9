package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"slices"
)

// Token sources reported by DiagnoseGitHub.
const (
	sourceHostsFile = "gh hosts file"
	sourceKeyring   = "OS keyring"
)

// HostCredential names where a host's token comes from. Source is empty when the
// host has none. It never holds the token itself.
type HostCredential struct{ Host, Source string }

// SSHDiagnosis describes available SSH authentication without exposing key material.
type SSHDiagnosis struct {
	AgentReachable bool
	AgentKeys      int
	// KeyFiles lists the usable (unencrypted) default key files by name.
	KeyFiles   []string
	KnownHosts bool
}

// DiagnoseGitHub reports the token source for github.com, GH_HOST and every host
// in gh's hosts file, using the same lookup order as authentication.
func (c *Credentials) DiagnoseGitHub(ctx context.Context) []HostCredential {
	hosts := []string{dotCom}
	if host := canonicalHost(c.env("GH_HOST")); host != "" && !slices.Contains(hosts, host) {
		hosts = append(hosts, host)
	}
	known, _ := c.hostsFile()
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !slices.Contains(hosts, name) {
			hosts = append(hosts, name)
		}
	}
	report := make([]HostCredential, 0, len(hosts))
	for _, host := range hosts {
		_, source := c.lookup(ctx, host)
		report = append(report, HostCredential{Host: host, Source: source})
	}
	return report
}

// DiagnoseSSH reports the SSH agent, the usable default keys and known_hosts.
func (c *Credentials) DiagnoseSSH() SSHDiagnosis {
	diagnosis := SSHDiagnosis{AgentKeys: len(c.agentSigners()), AgentReachable: c.agent != nil}
	if home, err := c.home(); err == nil {
		for _, name := range sshKeyNames {
			if loadKey(filepath.Join(home, ".ssh", name)) != nil {
				diagnosis.KeyFiles = append(diagnosis.KeyFiles, name)
			}
		}
	}
	if files, err := c.knownHostsFiles(); err == nil {
		diagnosis.KnownHosts = slices.ContainsFunc(files, func(path string) bool {
			info, statErr := os.Stat(path)
			return statErr == nil && !info.IsDir()
		})
	}
	return diagnosis
}
