package gitclient

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"gopkg.in/yaml.v3"
)

const (
	dotCom             = "github.com"
	defaultKeyringWait = 2 * time.Second
	agentDeadline      = 30 * time.Second
	maxHostsFile       = 1 << 20
	maxKeyFile         = 1 << 20
)

var sshKeyNames = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// Credentials resolves authentication for one host at a time. It never runs a
// credential helper or the gh binary and never reads Git configuration.
//
// GitHub tokens come from GITHUB_TOKEN, GH_TOKEN, then gh's stored token.
// Enterprise tokens come from GH_ENTERPRISE_TOKEN, GITHUB_ENTERPRISE_TOKEN, then
// gh's stored token, and only for a host known to be Enterprise: GH_HOST, a
// host in gh's hosts.yml, or TrustedHosts. Any other host gets no token.
//
// On macOS the keyring library invokes /usr/bin/security, and on other Unixes it
// is skipped when no D-Bus session exists so that dbus-launch is never started.
//
// SSH uses the agent, then unencrypted ~/.ssh/id_ed25519, id_ecdsa and id_rsa,
// and verifies host keys against known_hosts.
type Credentials struct {
	// TrustedHosts names Enterprise hosts the user explicitly configured.
	TrustedHosts []string

	// Getenv, HomeDir and Keyring default to the operating system.
	Getenv  func(string) string
	HomeDir func() (string, error)
	Keyring func(service, user string) (string, error)
	// KeyringTimeout bounds the keyring lookups of one resolution.
	KeyringTimeout time.Duration

	keyringDown atomic.Bool
	agentOnce   sync.Once
	agent       agent.ExtendedAgent
	agentConn   net.Conn
}

var processCredentials = &Credentials{}

// SharedCredentials returns the process-wide operating-system Credentials, so
// default clients reuse one SSH agent connection.
func SharedCredentials() *Credentials { return processCredentials }

func (c *Credentials) env(name string) string {
	if c.Getenv != nil {
		return c.Getenv(name)
	}
	return os.Getenv(name)
}

func (c *Credentials) home() (string, error) {
	if c.HomeDir != nil {
		return c.HomeDir()
	}
	return os.UserHomeDir()
}

func canonicalHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "api.github.com" {
		return dotCom
	}
	return host
}

type hostEntry struct {
	OAuthToken string `yaml:"oauth_token"`
	User       string `yaml:"user"`
	Users      map[string]struct {
		OAuthToken string `yaml:"oauth_token"`
	} `yaml:"users"`
}

func (c *Credentials) hostsFile() (map[string]hostEntry, error) {
	dir := c.env("GH_CONFIG_DIR")
	if dir == "" {
		if xdg := c.env("XDG_CONFIG_HOME"); xdg != "" {
			dir = filepath.Join(xdg, "gh")
		} else {
			home, err := c.home()
			if err != nil {
				return nil, err
			}
			dir = filepath.Join(home, ".config", "gh")
		}
	}
	path := filepath.Join(dir, "hosts.yml")
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxHostsFile {
		return nil, fmt.Errorf("%s is too large", path)
	}
	// #nosec G304 -- this is gh's own configuration path.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hosts := map[string]hostEntry{}
	if err = yaml.Unmarshal(data, &hosts); err != nil {
		return nil, err
	}
	lowered := make(map[string]hostEntry, len(hosts))
	for name, entry := range hosts {
		lowered[canonicalHost(name)] = entry
	}
	return lowered, nil
}

// Token returns the token for host, or "" when none applies. It is empty for an
// unknown host even when an Enterprise token is set: a token is sent only to the
// host it belongs to. A repository's origin alone never makes a host known,
// because a cloned repository can name any host.
func (c *Credentials) Token(ctx context.Context, host string) string {
	token, _ := c.lookup(ctx, host)
	return token
}

// lookup returns host's token and a description of where it came from.
func (c *Credentials) lookup(ctx context.Context, host string) (token, source string) {
	host = canonicalHost(host)
	if host == "" {
		return "", ""
	}
	hosts, _ := c.hostsFile()
	entry, known := hosts[host]
	var names []string
	if host == dotCom {
		names = []string{"GITHUB_TOKEN", "GH_TOKEN"}
	} else {
		if !known && !c.trusted(host) {
			return "", ""
		}
		names = []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}
	}
	for _, name := range names {
		if value := c.env(name); value != "" {
			return value, name
		}
	}
	if entry.OAuthToken != "" {
		return entry.OAuthToken, sourceHostsFile
	}
	return c.storedToken(ctx, host, entry)
}

func (c *Credentials) trusted(host string) bool {
	if canonicalHost(c.env("GH_HOST")) == host {
		return true
	}
	return slices.ContainsFunc(c.TrustedHosts, func(t string) bool { return canonicalHost(t) == host })
}

// storedToken mirrors gh's lookup: the active user's keyring entry, that user's
// configured token, then the unkeyed keyring entry older gh versions wrote.
func (c *Credentials) storedToken(ctx context.Context, host string, entry hostEntry) (token, source string) {
	if entry.User != "" {
		if value := c.keyringGet(ctx, host, entry.User); value != "" {
			return value, sourceKeyring
		}
		if user, ok := entry.Users[entry.User]; ok && user.OAuthToken != "" {
			return user.OAuthToken, sourceHostsFile
		}
	}
	if value := c.keyringGet(ctx, host, ""); value != "" {
		return value, sourceKeyring
	}
	return "", ""
}

// keyringGet bounds a lookup so a hung keyring cannot stall a command. After one
// timeout this Credentials stops consulting the keyring.
func (c *Credentials) keyringGet(ctx context.Context, host, user string) string {
	if c.keyringDown.Load() {
		return ""
	}
	get := c.Keyring
	if get == nil {
		if !c.sessionBusExists() {
			return ""
		}
		get = keyring.Get
	}
	wait := c.KeyringTimeout
	if wait <= 0 {
		wait = defaultKeyringWait
	}
	found := make(chan string, 1)
	go func() {
		token, err := get("gh:"+host, user)
		if err != nil {
			token = ""
		}
		found <- token
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case token := <-found:
		return token
	case <-timer.C:
		c.keyringDown.Store(true)
		return ""
	case <-ctx.Done():
		return ""
	}
}

// sessionBusExists reports whether the D-Bus client can connect without falling
// back to starting dbus-launch.
func (c *Credentials) sessionBusExists() bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return true
	}
	if c.env("DBUS_SESSION_BUS_ADDRESS") != "" {
		return true
	}
	runtimeDir := c.env("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	info, err := os.Stat(filepath.Join(runtimeDir, "bus"))
	return err == nil && info.Mode()&os.ModeSocket != 0
}

// auth returns the go-git auth method for t. It is nil for anonymous access.
func (c *Credentials) auth(ctx context.Context, op string, t target, required bool) (transport.AuthMethod, error) {
	switch t.endpoint.Protocol {
	case schemeHTTPS, schemeHTTP:
		var token string
		if t.endpoint.Protocol == schemeHTTPS || isLoopback(t.host()) {
			token = c.Token(ctx, t.host())
		}
		// Embedded credentials belong to exactly this origin. A bare username has no
		// secret of its own, so a configured token takes precedence over it.
		if t.password != "" || (t.username != "" && token == "") {
			return &githttp.BasicAuth{Username: t.username, Password: t.password}, nil
		}
		if token != "" {
			return &githttp.BasicAuth{Username: "x-access-token", Password: token}, nil
		}
		if required && t.endpoint.Protocol == schemeHTTPS {
			return nil, newError(KindAuth, op, "", fmt.Errorf("no credentials available for %s", t.host()))
		}
		return nil, nil
	case schemeSSH:
		method, err := c.sshAuth(op, t)
		if err != nil {
			return nil, err
		}
		return method, nil
	}
	return nil, nil
}

func (c *Credentials) knownHostsFiles() ([]string, error) {
	if files := filepath.SplitList(c.env("SSH_KNOWN_HOSTS")); len(files) > 0 {
		return files, nil
	}
	home, err := c.home()
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(home, ".ssh", "known_hosts"), "/etc/ssh/ssh_known_hosts"}, nil
}

func (c *Credentials) sshAuth(op string, t target) (*gitssh.PublicKeysCallback, error) {
	files, err := c.knownHostsFiles()
	if err != nil {
		return nil, newError(KindAuth, op, "", err)
	}
	db, err := gitssh.NewKnownHostsDb(files...)
	if err != nil {
		return nil, newError(KindAuth, op, "", err)
	}
	user := t.endpoint.User
	if user == "" {
		user = "git"
	}
	port := t.endpoint.Port
	if port == 0 {
		port = 22
	}
	return &gitssh.PublicKeysCallback{
		User:     user,
		Callback: c.signers,
		HostKeyCallbackHelper: gitssh.HostKeyCallbackHelper{
			HostKeyCallback:   db.HostKeyCallback(),
			HostKeyAlgorithms: db.HostKeyAlgorithms(net.JoinHostPort(t.endpoint.Host, strconv.Itoa(port))),
		},
	}, nil
}

func (c *Credentials) signers() ([]ssh.Signer, error) {
	found := c.agentSigners()
	home, err := c.home()
	if err != nil {
		return found, nil
	}
	for _, name := range sshKeyNames {
		// #nosec G304 -- fixed key file names in the user's own ~/.ssh.
		if signer := loadKey(filepath.Join(home, ".ssh", name)); signer != nil {
			found = append(found, signer)
		}
	}
	return found, nil
}

// loadKey returns nil for a missing, oversized, malformed or encrypted key.
func loadKey(path string) ssh.Signer {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxKeyFile {
		return nil
	}
	// #nosec G304 -- caller passes a fixed key file name.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil
	}
	return signer
}

// agentSigners shares one agent connection for the life of this Credentials.
func (c *Credentials) agentSigners() []ssh.Signer {
	c.agentOnce.Do(func() {
		socket := c.env("SSH_AUTH_SOCK")
		if socket == "" {
			return
		}
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.Dial("unix", socket)
		if err != nil {
			return
		}
		c.agentConn = conn
		c.agent = agent.NewClient(conn)
	})
	if c.agent == nil {
		return nil
	}
	_ = c.agentConn.SetDeadline(time.Now().Add(agentDeadline))
	signers, err := c.agent.Signers()
	if err != nil {
		return nil
	}
	return signers
}
