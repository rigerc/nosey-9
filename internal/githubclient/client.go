// Package githubclient is Nosey's native GitHub adapter over go-github. It runs
// no gh subprocess and its exported API exposes no go-github types.
package githubclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

const (
	dotCom         = "github.com"
	defaultTimeout = 30 * time.Second
)

// Kinds reuse gitclient's error classification; these add API-only cases.
const (
	KindRateLimited gitclient.Kind = "GitHub rate limit exceeded"
	KindInvalid     gitclient.Kind = "GitHub rejected the request"
)

// Error is a classified API failure.
type Error struct {
	Kind   gitclient.Kind
	Op     string
	Status int
	// RateReset is when a rate limit lifts, if known.
	RateReset time.Time
	Err       error
}

func (e *Error) Error() string {
	message := e.Op + ": " + string(e.Kind)
	if e.Status != 0 {
		message += fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// Is matches on the error's Kind, with gitclient kinds as targets.
func (e *Error) Is(target error) bool {
	return target == error(e.Kind)
}

// Options configure a Client.
type Options struct {
	// Host is the GitHub host; empty means github.com. An Enterprise host is the
	// origin host of the repository being operated on.
	Host string
	// Credentials resolves the token; nil uses the operating system's.
	Credentials *gitclient.Credentials
	// RequireAuth fails construction when no token is available. Use it for writes.
	RequireAuth bool
	// Timeout bounds each API request; zero uses 30 seconds.
	Timeout time.Duration
	// BaseURL replaces the API base URL. Test seam for httptest.
	BaseURL string
}

// Client is a token-scoped GitHub API client for one host.
type Client struct {
	api   *github.Client
	authn bool
}

// New resolves the host's token and constructs a Client. An Enterprise host gets
// a token only when gh knows it, GH_HOST names it, or Credentials.TrustedHosts
// lists it.
func New(ctx context.Context, options Options) (*Client, error) {
	host := strings.ToLower(strings.TrimSpace(options.Host))
	if host == "" || host == "api.github.com" {
		host = dotCom
	}
	if strings.ContainsAny(host, "/\\@?#") {
		return nil, &Error{Kind: gitclient.KindInvalidSource, Op: "connect", Err: fmt.Errorf("invalid GitHub host %q", host)}
	}
	credentials := options.Credentials
	if credentials == nil {
		credentials = &gitclient.Credentials{}
	}
	token := credentials.Token(ctx, host)
	if token == "" && options.RequireAuth {
		return nil, &Error{Kind: gitclient.KindAuth, Op: "connect", Err: fmt.Errorf("no GitHub token available for %s; set GITHUB_TOKEN (GH_ENTERPRISE_TOKEN with GH_HOST for Enterprise) or run gh auth login", host)}
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	clientOptions := []github.ClientOptionsFunc{github.WithTimeout(timeout), github.WithUserAgent("nosey")}
	if token != "" {
		clientOptions = append(clientOptions, github.WithAuthToken(token))
	}
	baseURL, uploadURL := endpoints(host)
	switch {
	case options.BaseURL != "":
		base := options.BaseURL
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		clientOptions = append(clientOptions, github.WithURLs(&base, &base))
	case host != dotCom:
		clientOptions = append(clientOptions, github.WithEnterpriseURLs(baseURL, uploadURL))
	}
	api, err := github.NewClient(clientOptions...)
	if err != nil {
		return nil, &Error{Kind: gitclient.KindFailed, Op: "connect", Err: err}
	}
	return &Client{api: api, authn: token != ""}, nil
}

// endpoints returns host's API and upload URLs. Host may carry a port.
func endpoints(host string) (string, string) {
	bare := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		bare = h
	}
	if strings.HasSuffix(bare, ".ghe.com") {
		return "https://api." + host + "/", "https://uploads." + host + "/"
	}
	return "https://" + host + "/", "https://" + host + "/"
}

// Authenticated reports whether the client holds a token.
func (c *Client) Authenticated() bool { return c.authn }

func classify(ctx context.Context, op string, response *github.Response, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return &Error{Kind: gitclient.KindCanceled, Op: op, Err: ctx.Err()}
	}
	result := &Error{Op: op, Kind: gitclient.KindFailed, Err: err}
	if response != nil && response.Response != nil {
		result.Status = response.StatusCode
	}
	var rate *github.RateLimitError
	var abuse *github.AbuseRateLimitError
	switch {
	case errors.As(err, &rate):
		result.Kind = KindRateLimited
		result.RateReset = rate.Rate.Reset.Time
	case errors.As(err, &abuse):
		result.Kind = KindRateLimited
		if abuse.RetryAfter != nil {
			result.RateReset = time.Now().Add(*abuse.RetryAfter)
		}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		result.Kind = gitclient.KindCanceled
	default:
		switch result.Status {
		case http.StatusUnauthorized:
			result.Kind = gitclient.KindAuth
		case http.StatusForbidden:
			result.Kind = gitclient.KindPermission
		case http.StatusNotFound:
			result.Kind = gitclient.KindMissingRepository
		case http.StatusUnprocessableEntity:
			result.Kind = KindInvalid
		}
	}
	return result
}

// Repository describes a GitHub repository.
type Repository struct {
	Owner         string
	Name          string
	Private       bool
	DefaultBranch string
	HTMLURL       string
	CloneURL      string
	SSHURL        string
}

func toRepository(repo *github.Repository) Repository {
	result := Repository{
		Name:          repo.GetName(),
		Private:       repo.GetPrivate(),
		DefaultBranch: repo.GetDefaultBranch(),
		HTMLURL:       repo.GetHTMLURL(),
		CloneURL:      repo.GetCloneURL(),
		SSHURL:        repo.GetSSHURL(),
	}
	if repo.Owner != nil {
		result.Owner = repo.Owner.GetLogin()
	}
	return result
}

// GetRepository returns owner/name.
func (c *Client) GetRepository(ctx context.Context, owner, name string) (Repository, error) {
	repo, response, err := c.api.Repositories.Get(ctx, owner, name)
	if err != nil {
		return Repository{}, classify(ctx, "get repository", response, err)
	}
	return toRepository(repo), nil
}

// CreateRepositoryRequest describes a new repository.
type CreateRepositoryRequest struct {
	Owner   string
	Name    string
	Private bool
}

// CreateRepository creates a repository for the authenticated user when Owner is
// that user or empty, otherwise in the organization named by Owner.
func (c *Client) CreateRepository(ctx context.Context, request CreateRepositoryRequest) (Repository, error) {
	const op = "create repository"
	organization := ""
	if request.Owner != "" {
		user, response, err := c.api.Users.Get(ctx, "")
		if err != nil {
			return Repository{}, classify(ctx, op, response, err)
		}
		if !strings.EqualFold(user.GetLogin(), request.Owner) {
			organization = request.Owner
		}
	}
	private := request.Private
	created, response, err := c.api.Repositories.Create(ctx, organization, github.CreateRepositoryRequest{Name: request.Name, Private: &private})
	if err != nil {
		return Repository{}, classify(ctx, op, response, err)
	}
	return toRepository(created), nil
}

// WorkflowPermissions are a repository's default GitHub Actions permissions.
type WorkflowPermissions struct {
	// Default is "read" or "write".
	Default string
	// CanApprovePullRequests lets Actions open and approve pull requests.
	CanApprovePullRequests bool
}

// UpdateDefaultWorkflowPermissions sets the repository's Actions defaults.
func (c *Client) UpdateDefaultWorkflowPermissions(ctx context.Context, owner, name string, permissions WorkflowPermissions) error {
	value, canApprove := permissions.Default, permissions.CanApprovePullRequests
	_, response, err := c.api.Repositories.UpdateDefaultWorkflowPermissions(ctx, owner, name, github.DefaultWorkflowPermissionRepository{
		DefaultWorkflowPermissions:   &value,
		CanApprovePullRequestReviews: &canApprove,
	})
	return classify(ctx, "update workflow permissions", response, err)
}

// PullRequest is an open pull request.
type PullRequest struct {
	Number  int
	URL     string
	Head    string
	Base    string
	Title   string
	IsDraft bool
}

func toPullRequest(pr *github.PullRequest) PullRequest {
	return PullRequest{
		Number:  pr.GetNumber(),
		URL:     pr.GetHTMLURL(),
		Head:    pr.GetHead().GetRef(),
		Base:    pr.GetBase().GetRef(),
		Title:   pr.GetTitle(),
		IsDraft: pr.GetDraft(),
	}
}

// CreatePullRequestRequest describes a new pull request.
type CreatePullRequestRequest struct {
	Owner string
	Repo  string
	Base  string
	Head  string
	Title string
	Body  string
}

// CreatePullRequest opens a pull request.
func (c *Client) CreatePullRequest(ctx context.Context, request CreatePullRequestRequest) (PullRequest, error) {
	title, body := request.Title, request.Body
	created, response, err := c.api.PullRequests.Create(ctx, request.Owner, request.Repo, github.CreatePullRequest{
		Title: &title,
		Head:  request.Head,
		Base:  request.Base,
		Body:  &body,
	})
	if err != nil {
		return PullRequest{}, classify(ctx, "create pull request", response, err)
	}
	return toPullRequest(created), nil
}

// FindPullRequest returns the open pull request from head into base. Found is
// false when there is none. The head filter uses the repository owner.
func (c *Client) FindPullRequest(ctx context.Context, owner, repo, head, base string) (PullRequest, bool, error) {
	pulls, response, err := c.api.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{State: "open", Head: owner + ":" + head, Base: base})
	if err != nil {
		return PullRequest{}, false, classify(ctx, "find pull request", response, err)
	}
	if len(pulls) == 0 {
		return PullRequest{}, false, nil
	}
	return toPullRequest(pulls[0]), true, nil
}
