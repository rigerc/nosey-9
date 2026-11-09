package gitclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cgi" //nolint:gosec // test fixture serves git-http-backend; Httpoxy does not apply
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const testToken = "s3cret-token"

func tokenCredentials(t *testing.T, env map[string]string, trusted ...string) *Credentials {
	t.Helper()
	credentials := emptyCredentials(t.TempDir())
	credentials.Getenv = func(name string) string { return env[name] }
	credentials.TrustedHosts = trusted
	return credentials
}

func httpBackend(t *testing.T, root string) http.Handler { //nolint:antislop // net/http/httptest.NewServer takes an http.Handler
	t.Helper()
	return &cgi.Handler{
		Path:       filepath.Join(gitCmd(t, root, "--exec-path"), "git-http-backend"),
		Root:       "/",
		Env:        []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=test"},
		InheritEnv: []string{"PATH"},
	}
}

// requestLog records the Authorization header of every request.
type requestLog struct {
	mu      sync.Mutex
	headers []string
}

func (l *requestLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.headers = append(l.headers, r.Header.Get("Authorization"))
}

func (l *requestLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.headers...)
}

func TestNativeAuthHTTPS(t *testing.T) {
	t.Run("token is presented over the wire and writes succeed", func(t *testing.T) {
		f := newFixture(t, anySHA, "http.receivepack=true")
		backend := httpBackend(t, filepath.Dir(f.bare))
		log := &requestLog{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.record(r)
			user, password, ok := r.BasicAuth()
			if !ok || user != "x-access-token" || password != testToken {
				w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			backend.ServeHTTP(w, r)
		}))
		defer server.Close()
		url := server.URL + "/origin.git"
		policy := Policy{AllowHTTP: true, TrustedOrigin: true}

		c := New(Options{Policy: policy, Credentials: tokenCredentials(t, map[string]string{"GH_ENTERPRISE_TOKEN": testToken}, "127.0.0.1"), TempDir: t.TempDir()})
		repo, err := c.Temporary(ctx, url, TemporaryOptions{})
		require.NoError(t, err)
		defer func() { _ = repo.Close() }()
		commit, err := repo.FetchRef(ctx, "main")
		require.NoError(t, err)
		require.Equal(t, f.commits[2], commit)
		require.NoError(t, repo.Push(ctx, commit, "authed"))
		require.Equal(t, commit, gitCmd(t, f.bare, "rev-parse", "refs/heads/authed"))
		require.NotEmpty(t, log.seen())

		anonymous := New(Options{Policy: policy, Credentials: emptyCredentials(t.TempDir())})
		_, err = anonymous.ResolveRemote(ctx, url, "main")
		require.ErrorIs(t, err, KindAuth, "a 401 is a typed auth error")
	})

	t.Run("public read needs no token", func(t *testing.T) {
		f := newFixture(t)
		backend := httpBackend(t, filepath.Dir(f.bare))
		log := &requestLog{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.record(r)
			backend.ServeHTTP(w, r)
		}))
		defer server.Close()
		c := New(Options{Policy: Policy{AllowHTTP: true, TrustedOrigin: true}, Credentials: emptyCredentials(t.TempDir())})
		found, err := c.ResolveRemote(ctx, server.URL+"/origin.git", "main")
		require.NoError(t, err)
		require.Equal(t, f.commits[2], found.Commit)
		for _, header := range log.seen() {
			require.Empty(t, header)
		}
	})

	t.Run("status codes are typed", func(t *testing.T) {
		for status, kind := range map[int]Kind{http.StatusForbidden: KindPermission, http.StatusNotFound: KindMissingRepository} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			c := New(Options{Policy: Policy{AllowHTTP: true, TrustedOrigin: true}, Credentials: emptyCredentials(t.TempDir())})
			_, err := c.ResolveRemote(ctx, server.URL+"/o.git", "main")
			server.Close()
			require.ErrorIs(t, err, kind, "status %d", status)
		}
	})

	t.Run("https selects the token by host", func(t *testing.T) {
		env := map[string]string{"GITHUB_TOKEN": "first", "GH_TOKEN": "second"}
		target, err := Hardened().resolve("owner/repo")
		require.NoError(t, err)
		method, err := tokenCredentials(t, env).auth(ctx, "op", target, true)
		require.NoError(t, err)
		require.Equal(t, &githttp.BasicAuth{Username: "x-access-token", Password: "first"}, method)

		delete(env, "GITHUB_TOKEN")
		method, err = tokenCredentials(t, env).auth(ctx, "op", target, true)
		require.NoError(t, err)
		require.Equal(t, "second", method.(*githttp.BasicAuth).Password) //nolint:antislop // the preceding assertion established the concrete type
	})

	t.Run("gh stored tokens", func(t *testing.T) {
		configDir := t.TempDir()
		writeFile(t, filepath.Join(configDir, "hosts.yml"), "github.com:\n  oauth_token: plain\nghe.example.test:\n  user: me\n")
		env := map[string]string{"GH_CONFIG_DIR": configDir}
		credentials := tokenCredentials(t, env)
		require.Equal(t, "plain", credentials.Token(ctx, "GitHub.com"))
		var asked []string
		credentials.Keyring = func(service, user string) (string, error) {
			asked = append(asked, service+"|"+user)
			return "from-keyring", nil
		}
		require.Equal(t, "from-keyring", credentials.Token(ctx, "ghe.example.test"))
		require.Equal(t, []string{"gh:ghe.example.test|me"}, asked)
	})

	t.Run("a hung keyring cannot stall", func(t *testing.T) {
		configDir := t.TempDir()
		writeFile(t, filepath.Join(configDir, "hosts.yml"), "github.com:\n  user: me\n")
		release := make(chan struct{})
		defer close(release)
		credentials := tokenCredentials(t, map[string]string{"GH_CONFIG_DIR": configDir})
		credentials.KeyringTimeout = 10 * time.Millisecond
		credentials.Keyring = func(string, string) (string, error) { <-release; return "late", nil }
		started := time.Now()
		require.Empty(t, credentials.Token(ctx, "github.com"))
		require.Less(t, time.Since(started), 5*time.Second)
	})

	t.Run("writes require a token", func(t *testing.T) {
		c := New(Options{Credentials: emptyCredentials(t.TempDir()), TempDir: t.TempDir()})
		repo, err := c.Temporary(ctx, "https://github.com/o/r", TemporaryOptions{})
		require.NoError(t, err)
		defer func() { _ = repo.Close() }()
		commit := strings.Repeat("a", 40)
		require.ErrorIs(t, repo.Push(ctx, commit, "b"), KindAuth)
		require.ErrorIs(t, repo.PushLease(ctx, commit, "b", commit), KindAuth)
	})
}

func TestCredentialNotForwardedToDifferentOrigin(t *testing.T) {
	everything := map[string]string{"GITHUB_TOKEN": testToken, "GH_TOKEN": testToken, "GH_ENTERPRISE_TOKEN": testToken, "GITHUB_ENTERPRISE_TOKEN": testToken}

	t.Run("tokens stay with their host", func(t *testing.T) {
		credentials := tokenCredentials(t, everything, "ghe.example.test")
		require.Equal(t, testToken, credentials.Token(ctx, "github.com"))
		require.Equal(t, testToken, credentials.Token(ctx, "ghe.example.test"))
		for _, host := range []string{"evil.test", "github.com.evil.test", "evilgithub.com", "gitlab.com", ""} {
			require.Empty(t, credentials.Token(ctx, host), host)
		}
	})

	t.Run("a user origin does not vouch for its host", func(t *testing.T) {
		target, err := UserOrigin().resolve("https://evil.test/o/r")
		require.NoError(t, err)
		method, err := tokenCredentials(t, everything).auth(ctx, "op", target, false)
		require.NoError(t, err)
		require.Nil(t, method)
	})

	t.Run("no token over plain http", func(t *testing.T) {
		target, err := UserOrigin().resolve("http://github.com/o/r")
		require.NoError(t, err)
		method, err := tokenCredentials(t, everything).auth(ctx, "op", target, false)
		require.NoError(t, err)
		require.Nil(t, method)
	})

	f := newFixture(t)
	backend := httpBackend(t, filepath.Dir(f.bare))
	for name, rewrite := range map[string]func(string) string{
		"redirect to another port": func(url string) string { return url },
		"redirect to another host": func(url string) string { return strings.Replace(url, "127.0.0.1", "localhost", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			destination := &requestLog{}
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destination.record(r)
				backend.ServeHTTP(w, r)
			}))
			defer other.Close()
			source := &requestLog{}
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				source.record(r)
				http.Redirect(w, r, rewrite(other.URL)+r.URL.RequestURI(), http.StatusFound) //nolint:gosec // the test server redirects to its own fixture URL
			}))
			defer first.Close()
			c := New(Options{Policy: Policy{AllowHTTP: true, TrustedOrigin: true}, Credentials: tokenCredentials(t, map[string]string{"GH_ENTERPRISE_TOKEN": testToken}, "127.0.0.1")})
			_, _ = c.ResolveRemote(ctx, first.URL+"/origin.git", "main")
			require.NotEmpty(t, source.seen())
			require.NotEmpty(t, source.seen()[0], "the token reaches its own origin")
			require.NotEmpty(t, destination.seen(), "the redirect was followed")
			for _, header := range destination.seen() {
				require.Empty(t, header, "the token must not follow a cross-origin redirect")
			}
		})
	}
}

// sshServer serves git-upload-pack to one authorized key.
type sshServer struct {
	addr string
	host ssh.PublicKey
}

func startSSHServer(t *testing.T, authorized ssh.PublicKey) sshServer {
	t.Helper()
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	require.NoError(t, err)
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(key.Marshal(), authorized.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("unauthorized key")
	}}
	config.AddHostKey(hostSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go serveSSH(conn, config)
		}
	}()
	return sshServer{addr: listener.Addr().String(), host: hostSigner.PublicKey()}
}

func serveSSH(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }()
	go ssh.DiscardRequests(requests)
	for candidate := range channels {
		channel, channelRequests, acceptErr := candidate.Accept()
		if acceptErr != nil {
			return
		}
		go serveSession(channel, channelRequests)
	}
}

func serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() { _ = channel.Close() }()
	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		_ = ssh.Unmarshal(request.Payload, &payload)
		_ = request.Reply(true, nil)
		service, repository, _ := strings.Cut(payload.Command, " ")
		command := exec.Command("git", strings.TrimPrefix(service, "git-"), strings.Trim(repository, "'")) //nolint:gosec // test fixture running git with test-controlled arguments
		command.Stdin, command.Stdout, command.Stderr = channel, channel, channel.Stderr()
		command.WaitDelay = 2 * time.Second
		_ = command.Run()
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}

func newSigner(t *testing.T) (ed25519.PrivateKey, ssh.Signer) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	return private, signer
}

func sshCredentials(t *testing.T, server sshServer, env map[string]string) (*Credentials, string) {
	t.Helper()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".ssh/known_hosts"), knownhosts.Line([]string{knownhosts.Normalize(server.addr)}, server.host)+"\n")
	credentials := emptyCredentials(home)
	credentials.Getenv = func(name string) string { return env[name] }
	return credentials, home
}

func TestNativeAuthSSH(t *testing.T) {
	f := newFixture(t)
	private, signer := newSigner(t)
	server := startSSHServer(t, signer.PublicKey())
	url := fmt.Sprintf("ssh://git@%s%s", server.addr, f.bare)
	writeKey := func(t *testing.T, home, name string, key ed25519.PrivateKey, passphrase string) {
		t.Helper()
		var block *pem.Block
		var err error
		if passphrase == "" {
			block, err = ssh.MarshalPrivateKey(key, "")
		} else {
			block, err = ssh.MarshalPrivateKeyWithPassphrase(key, "", []byte(passphrase))
		}
		require.NoError(t, err)
		writeFile(t, filepath.Join(home, ".ssh", name), string(pem.EncodeToMemory(block)))
	}
	resolve := func(credentials *Credentials) (RemoteRef, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return New(Options{Policy: UserOrigin(), Credentials: credentials}).ResolveRemote(ctx, url, "main")
	}

	t.Run("key file", func(t *testing.T) {
		credentials, home := sshCredentials(t, server, nil)
		writeKey(t, home, "id_ed25519", private, "")
		found, err := resolve(credentials)
		require.NoError(t, err)
		require.Equal(t, f.commits[2], found.Commit)
	})

	t.Run("full fetch over ssh", func(t *testing.T) {
		credentials, home := sshCredentials(t, server, nil)
		writeKey(t, home, "id_ed25519", private, "")
		c := New(Options{Policy: UserOrigin(), Credentials: credentials, TempDir: t.TempDir()})
		repo := tempRepo(t, c, url, TemporaryOptions{})
		commit, err := repo.FetchRef(ctx, "main")
		require.NoError(t, err)
		require.Equal(t, f.commits[2], commit)
	})

	t.Run("agent", func(t *testing.T) {
		socketDir, err := os.MkdirTemp("", "agent")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
		socket := filepath.Join(socketDir, "s")
		listener, err := net.Listen("unix", socket)
		require.NoError(t, err)
		defer func() { _ = listener.Close() }()
		keyring := agent.NewKeyring()
		require.NoError(t, keyring.Add(agent.AddedKey{PrivateKey: private}))
		go func() {
			for {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				go func() { _ = agent.ServeAgent(keyring, conn) }()
			}
		}()
		credentials, _ := sshCredentials(t, server, map[string]string{"SSH_AUTH_SOCK": socket})
		found, err := resolve(credentials)
		require.NoError(t, err)
		require.Equal(t, f.commits[2], found.Commit)
	})

	t.Run("encrypted key is not usable", func(t *testing.T) {
		credentials, home := sshCredentials(t, server, nil)
		writeKey(t, home, "id_ed25519", private, "passphrase")
		_, err := resolve(credentials)
		require.ErrorIs(t, err, KindAuth)
	})

	t.Run("unauthorized key", func(t *testing.T) {
		other, _ := newSigner(t)
		credentials, home := sshCredentials(t, server, nil)
		writeKey(t, home, "id_ed25519", other, "")
		_, err := resolve(credentials)
		require.ErrorIs(t, err, KindAuth)
	})

	t.Run("unknown host key is refused", func(t *testing.T) {
		credentials, home := sshCredentials(t, server, nil)
		writeKey(t, home, "id_ed25519", private, "")
		_, wrong := newSigner(t)
		writeFile(t, filepath.Join(home, ".ssh/known_hosts"), knownhosts.Line([]string{knownhosts.Normalize(server.addr)}, wrong.PublicKey())+"\n")
		_, err := resolve(credentials)
		require.ErrorIs(t, err, KindAuth)
	})

	t.Run("missing known_hosts is refused", func(t *testing.T) {
		home := t.TempDir()
		writeKey(t, home, "id_ed25519", private, "")
		_, err := resolve(emptyCredentials(home))
		require.ErrorIs(t, err, KindAuth)
	})
}
