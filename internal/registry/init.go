package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/githubclient"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

// InitOptions selects the published empty template and explicit output actions.
type InitOptions struct {
	Path, RegistryID, Version string
	// CI adds the template's GitHub Actions workflow.
	CI bool
	// GitHubRepo, as OWNER/NAME, creates and pushes a GitHub repository, which needs a token.
	GitHubRepo string
	// Public makes the GitHub repository public; it is private otherwise.
	Public        bool
	Register, Git bool
}

// defaultBranch is the branch the scaffold's workflow triggers on.
const defaultBranch = "main"

const (
	initialCommitMessage = "Initial registry"
	// publishTimeout bounds the GitHub half of Init, including the push.
	publishTimeout = 5 * time.Minute
)

var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)

// ValidGitHubRepo reports whether repo is an OWNER/NAME GitHub repository slug.
func ValidGitHubRepo(repo string) bool { return githubRepoPattern.MatchString(repo) }

// InitResult is the versioned registry-generation envelope.
type InitResult struct {
	Version          int                 `json:"outputVersion"`
	Diagnostics      []domain.Diagnostic `json:"diagnostics"`
	Path             string              `json:"path"`
	RegistryID       string              `json:"registryId"`
	TemplateID       string              `json:"templateId"`
	SourceRegistryID string              `json:"sourceRegistryId"`
	Revision         *string             `json:"sourceRevision"`
	Files            []string            `json:"createdFiles"`
	Valid            bool                `json:"valid"`
	CreatedBy        string              `json:"createdBy"`
	// PublishedURL is the repository's web address after a GitHub publish; it is
	// reported to the terminal and is not part of the JSON output.
	PublishedURL string `json:"-"`
}

type templateCIFile struct {
	Provider    string `json:"provider"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type templateDescriptor struct {
	Generator string `json:"generator"`
	Files     []struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	} `json:"files"`
	CI []templateCIFile `json:"ci"`
}

// Init stages and validates data-only template output before publication.
func (s Service) Init(ctx context.Context, c domain.Catalog, opts InitOptions) (InitResult, error) {
	const templateID = "empty"
	result := InitResult{Version: 2, Diagnostics: []domain.Diagnostic{}, Files: []string{}, TemplateID: templateID, SourceRegistryID: opts.RegistryID, CreatedBy: opts.Version}
	if result.SourceRegistryID == "" {
		result.SourceRegistryID = "nosey:official"
	}
	if opts.GitHubRepo != "" && !ValidGitHubRepo(opts.GitHubRepo) {
		return result, fmt.Errorf("invalid GitHub repository %q (expected OWNER/NAME)", opts.GitHubRepo)
	}
	if opts.Public && opts.GitHubRepo == "" {
		return result, fmt.Errorf("public visibility requires a GitHub repository")
	}
	root, ok := c.Roots[result.SourceRegistryID]
	if !ok {
		return result, fmt.Errorf("mandatory template registry unavailable")
	}
	manifest, err := readManifest(root)
	if err != nil {
		return result, err
	}
	result.Revision = c.Revisions[result.SourceRegistryID]
	index := slices.IndexFunc(manifest.Templates, func(t domain.Template) bool { return t.ID == templateID && t.Kind == "registry" })
	if index < 0 {
		return result, fmt.Errorf("template %s not published by registry %s", templateID, result.SourceRegistryID)
	}
	descriptor := manifest.Templates[index].Descriptor
	template, err := readTemplate(root, descriptor)
	if err != nil {
		return result, err
	}
	target, err := filepath.Abs(opts.Path)
	if err != nil {
		return result, err
	}
	if _, err = os.Lstat(target); err == nil {
		return result, fmt.Errorf("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".nosey-init-")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	base := filepath.Dir(descriptor)
	seen := map[string]bool{}
	total := 0
	copyFile := func(source, destination string) error {
		if !filepath.IsLocal(destination) || !filepath.IsLocal(source) || seen[destination] || strings.Contains(destination, "\\") || strings.Contains(source, "\\") {
			return fmt.Errorf("unsafe or duplicate template path")
		}
		seen[destination] = true
		bytes, err := storage.Read(root, filepath.Join(base, source), 1<<20)
		if err != nil {
			return err
		}
		total += len(bytes)
		if total > 8<<20 || len(seen) > 100 {
			return fmt.Errorf("template resource limit exceeded")
		}
		return storage.Write(filepath.Join(stage, destination), bytes)
	}
	for _, file := range template.Files {
		if err = copyFile(file.Source, file.Destination); err != nil {
			return result, err
		}
		result.Files = append(result.Files, file.Destination)
	}
	record := scaffold{Version: scaffoldVersion, Template: templateID, Source: result.SourceRegistryID, Revision: result.Revision, CreatedBy: opts.Version}
	if opts.CI {
		for _, ci := range template.CI {
			if err = checkCIFile(ci.Provider, ci.Destination); err != nil {
				return result, err
			}
			if err = copyFile(ci.Source, ci.Destination); err != nil {
				return result, err
			}
			result.Files = append(result.Files, ci.Destination)
			var copied []byte
			if copied, err = storage.Read(stage, ci.Destination, 1<<20); err != nil {
				return result, err
			}
			record.recordCI(ci.Provider, ci.Destination, copied)
		}
	}
	if _, err = os.Stat(filepath.Join(stage, registrygen.ConfigName)); err != nil {
		return result, fmt.Errorf("template registry %s provides an outdated %q template without %s; run `nosey registry update` to refresh it", result.SourceRegistryID, templateID, registrygen.ConfigName)
	}
	id := registryID(target, opts.GitHubRepo)
	if err = rewriteConfig(stage, id, filepath.Base(target)); err != nil {
		return result, err
	}
	result.RegistryID = id
	if _, err = registrygen.Run(stage, registrygen.Options{Command: "generate"}); err != nil {
		return result, err
	}
	generated, err := readManifest(stage)
	if err != nil {
		return result, err
	}
	if err = storage.JSON(filepath.Join(stage, scaffoldName), record); err != nil {
		return result, err
	}
	result.Files = append(result.Files, scaffoldName)
	if err = validateSource(stage); err != nil {
		return result, err
	}
	checkSettings := settings.Defaults()
	checkSettings.Registries = []domain.RegistryRef{{ID: generated.ID, Source: domain.RegistrySource{Kind: "local", Path: stage}, Enabled: true}}
	checked, err := s.Load(ctx, settings.Resolved{Settings: checkSettings}, false)
	if err != nil {
		return result, err
	}
	for _, diagnostic := range checked.Diagnostics {
		if diagnostic.Severity == "error" {
			return result, fmt.Errorf("generated registry is invalid: %s", diagnostic.Message)
		}
	}
	if err = os.Rename(stage, target); err != nil {
		return result, err
	}
	result.Path = target
	result.Valid = true
	slices.Sort(result.Files)
	if opts.Register {
		refs, err := s.Store.UserRegistries()
		if err != nil {
			return result, err
		}
		refs = append(refs, domain.RegistryRef{ID: generated.ID, Source: domain.RegistrySource{Kind: "local", Path: target}, Enabled: true, Priority: 20})
		if err = s.Store.Registries(refs); err != nil {
			return result, fmt.Errorf("registry created; optional registration failed: %w", err)
		}
	}
	switch {
	case opts.GitHubRepo != "":
		api, published, err := s.publishGitHub(ctx, target, opts)
		if err != nil {
			return result, err
		}
		result.PublishedURL = published
		if opts.CI {
			if err = allowActionsPullRequests(ctx, api, opts.GitHubRepo); err != nil {
				result.Diagnostics = append(result.Diagnostics, domain.Diagnostic{
					Code:     "W_ACTIONS_PULL_REQUESTS",
					Severity: "warning",
					Message:  "Could not let GitHub Actions open pull requests; enable it under Settings > Actions > General, or regeneration PRs will fail: " + storage.Safe(err.Error()),
					Subject:  opts.GitHubRepo,
				})
			}
		}
	case opts.Git:
		if _, err = s.userGit().InitUser(target, defaultBranch); err != nil {
			return result, fmt.Errorf("registry created; optional git init failed: %w", err)
		}
	}
	return result, nil
}

// PublishStage names the step of a GitHub publish that failed.
type PublishStage string

// Publish stages, in order. The local registry exists at every stage.
const (
	PublishCommit     PublishStage = "commit"
	PublishAuth       PublishStage = "auth"
	PublishRepository PublishStage = "repository"
	PublishPush       PublishStage = "push"
	PublishTracking   PublishStage = "tracking"
)

// ErrRepositoryConflict marks an existing GitHub repository Init will not publish to.
var ErrRepositoryConflict = errors.New("repository does not match this registry")

// PublishError is a partial failure of a GitHub publish. It unwraps to the cause,
// so errors.Is matches gitclient kinds and ErrRepositoryConflict.
type PublishError struct {
	Stage      PublishStage
	Repository string
	Path       string
	summary    string
	hint       string
	Err        error
}

func (e *PublishError) Error() string {
	return e.summary + ": " + storage.Safe(e.Err.Error()) + "; " + e.hint
}

// Unwrap returns the cause.
func (e *PublishError) Unwrap() error { return e.Err }

// publishGitHub commits the new registry, creates or reuses its GitHub
// repository and pushes. It never deletes a remote repository or forces a push:
// after a failure the same command can be re-run from a fresh directory.
func (s Service) publishGitHub(ctx context.Context, dir string, opts InitOptions) (*githubclient.Client, string, error) {
	owner, name, _ := strings.Cut(opts.GitHubRepo, "/")
	retry := "remove " + dir + " and re-run this command"
	fail := func(stage PublishStage, summary, hint string, err error) error {
		return &PublishError{Stage: stage, Repository: opts.GitHubRepo, Path: dir, summary: summary, hint: hint, Err: err}
	}
	client := s.userGit()
	repo, err := client.InitUser(dir, defaultBranch)
	if err != nil {
		return nil, "", fail(PublishCommit, "registry created; Git initialization failed", retry, err)
	}
	identity, err := repo.Identity()
	if err != nil {
		return nil, "", fail(PublishCommit, "registry created; the initial commit needs a Git identity",
			`run git config --global user.name "NAME" and git config --global user.email "EMAIL", then `+retry, err)
	}
	commit, err := repo.Commit(ctx, gitclient.CommitOptions{Message: initialCommitMessage, Identity: identity})
	if err != nil {
		return nil, "", fail(PublishCommit, "registry created; the initial commit failed", retry, err)
	}

	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	options := s.GitHub
	if options.Credentials == nil {
		options.Credentials = gitclient.SharedCredentials()
	}
	if options.Host == "" {
		options.Host = os.Getenv("GH_HOST")
	}
	options.RequireAuth = true
	committed := "registry created and committed locally; "
	api, err := githubclient.New(ctx, options)
	if err != nil {
		return nil, "", fail(PublishAuth, committed+"cannot authenticate to GitHub", "provide a token, then "+retry, err)
	}
	remote, created, err := ensureRepository(ctx, api, owner, name, !opts.Public)
	if err != nil {
		return nil, "", fail(PublishRepository, committed+"GitHub repository creation failed",
			"check the token can create repositories under "+owner+", then "+retry, err)
	}
	if err = checkExisting(remote, created, owner, name, opts.Public); err != nil {
		return nil, "", fail(PublishRepository, committed+"GitHub repository "+opts.GitHubRepo+" already exists and was left unchanged",
			"choose another name, or change that repository and "+retry, err)
	}
	url := remote.CloneURL
	if url == "" {
		host := options.Host
		if host == "" {
			host = "github.com"
		}
		url = "https://" + host + "/" + opts.GitHubRepo + ".git"
	}
	published := ""
	if !created {
		if published, err = comparePublished(ctx, client, repo, commit, url); err != nil {
			return nil, "", fail(PublishRepository, committed+"GitHub repository "+opts.GitHubRepo+" already exists and was left unchanged",
				"choose another name, or empty that repository and "+retry, err)
		}
	}

	pushFailed := committed + "GitHub repository " + opts.GitHubRepo + " exists but the push failed"
	resume := "run git -C " + dir + " push -u origin " + defaultBranch + ", or " + retry + "; the repository is reused"
	if err = repo.SetOrigin(url); err != nil {
		return nil, "", fail(PublishPush, pushFailed, retry, err)
	}
	if published == "" {
		if err = repo.Push(ctx, commit, defaultBranch); err != nil {
			return nil, "", fail(PublishPush, pushFailed, pushAdvice(err, opts.GitHubRepo)+resume, err)
		}
		published = commit
	} else {
		if _, err = repo.FetchRef(ctx, remoteMainRef); err == nil {
			err = repo.AdoptCommit(ctx, published)
		}
		if err != nil {
			return nil, "", fail(PublishPush, committed+"GitHub repository "+opts.GitHubRepo+" already holds this registry but it could not be linked", retry, err)
		}
	}
	if err = repo.SetUpstream(defaultBranch); err == nil {
		err = repo.RecordPublished(published, defaultBranch)
	}
	if err != nil {
		return nil, "", fail(PublishTracking, "registry published to "+opts.GitHubRepo+"; local upstream tracking was not configured", "run git -C "+dir+" push -u origin "+defaultBranch, err)
	}
	return api, publishedURL(remote.HTMLURL, options.Host, opts.GitHubRepo), nil
}

// publishedURL prefers the address the API reported, which is correct on
// GitHub Enterprise; a response without one falls back to the API host.
func publishedURL(reported, host, repository string) string {
	if reported != "" {
		return reported
	}
	if host == "" {
		host = "github.com"
	}
	return "https://" + host + "/" + repository
}

const remoteMainRef = "refs/heads/" + defaultBranch

// ensureRepository returns the repository, creating it only when it is absent.
// A failed create re-reads it, since an earlier or concurrent run may have won.
func ensureRepository(ctx context.Context, api *githubclient.Client, owner, name string, private bool) (githubclient.Repository, bool, error) {
	existing, err := api.GetRepository(ctx, owner, name)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, gitclient.KindMissingRepository) {
		return githubclient.Repository{}, false, err
	}
	created, err := api.CreateRepository(ctx, githubclient.CreateRepositoryRequest{Owner: owner, Name: name, Private: private})
	if err == nil {
		return created, true, nil
	}
	if errors.Is(err, githubclient.KindInvalid) {
		if existing, getErr := api.GetRepository(ctx, owner, name); getErr == nil {
			return existing, false, nil
		}
	}
	return githubclient.Repository{}, false, err
}

// checkExisting refuses a reused repository with another identity (GitHub answers
// for a renamed repository under its new name) or the wrong visibility, because
// pushing to a public repository must never follow a request for a private one.
func checkExisting(remote githubclient.Repository, created bool, owner, name string, public bool) error {
	if created {
		return nil
	}
	if !strings.EqualFold(remote.Name, name) || (remote.Owner != "" && !strings.EqualFold(remote.Owner, owner)) {
		return fmt.Errorf("%w: it is now %s/%s", ErrRepositoryConflict, remote.Owner, remote.Name)
	}
	if remote.Private == public {
		visibility, requested := "public", "private"
		if remote.Private {
			visibility, requested = "private", "public"
		}
		return fmt.Errorf("%w: it is %s but a %s repository was requested", ErrRepositoryConflict, visibility, requested)
	}
	return nil
}

// comparePublished inspects an existing repository. It returns "" when the
// repository is empty and the remote commit when that commit holds exactly the
// local content, as after a previous run. Anything else is a conflict.
func comparePublished(ctx context.Context, client *gitclient.Client, repo *gitclient.Repo, commit, url string) (string, error) {
	head, err := client.ResolveRemote(ctx, url, "")
	if errors.Is(err, gitclient.KindMissingRef) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if head.Branch != defaultBranch {
		return "", fmt.Errorf("%w: its default branch is %q, not %q", ErrRepositoryConflict, head.Branch, defaultBranch)
	}
	remote, err := client.Temporary(ctx, url, gitclient.TemporaryOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = remote.Close() }()
	published, err := remote.FetchRef(ctx, remoteMainRef)
	if err != nil {
		return "", err
	}
	remoteTree, err := remote.TreeHash(ctx, published, "")
	if err != nil {
		return "", err
	}
	localTree, err := repo.TreeHash(ctx, commit, "")
	if err != nil {
		return "", err
	}
	if remoteTree != localTree {
		return "", fmt.Errorf("%w: its %s branch has different content", ErrRepositoryConflict, defaultBranch)
	}
	return published, nil
}

// pushAdvice explains a push failure. GitHub rejects workflow files from a token
// without the workflow scope only as text in the push report.
func pushAdvice(err error, repo string) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "refusing to allow") && strings.Contains(message, "workflow"):
		return "the token lacks the `workflow` scope needed to push .github/workflows (fine-grained tokens need Workflows write); run gh auth refresh -s workflow or use a token that has it, then "
	case errors.Is(err, gitclient.KindAuth), errors.Is(err, gitclient.KindPermission):
		return "the token was rejected or cannot write to " + repo + "; use a token with write access, then "
	}
	return "check the connection, then "
}

// allowActionsPullRequests lets the scaffold's workflow open review pull
// requests, which GitHub disables by default. GitHub exposes opening and
// approving as one setting.
func allowActionsPullRequests(ctx context.Context, api *githubclient.Client, repo string) error {
	owner, name, _ := strings.Cut(repo, "/")
	return api.UpdateDefaultWorkflowPermissions(ctx, owner, name, githubclient.WorkflowPermissions{Default: "read", CanApprovePullRequests: true})
}

// userGit returns the client for the repository Init creates for the user.
func (s Service) userGit() *gitclient.Client {
	if s.UserGit != nil {
		return s.UserGit
	}
	return gitclient.New(gitclient.Options{Policy: gitclient.UserOrigin(), Credentials: gitclient.SharedCredentials()})
}

// registryID names a published registry after its repository, so its identity
// is unique and does not claim to be local. IDs cannot contain "/".
func registryID(target, githubRepo string) string {
	if githubRepo != "" {
		return "github:" + strings.ReplaceAll(githubRepo, "/", ":")
	}
	return "local:" + regexp.MustCompile(`[^A-Za-z0-9._:-]+`).ReplaceAllString(filepath.Base(target), "-")
}

// rewriteConfig sets the staged registry identity and removes any seed source.
func rewriteConfig(stage, id, name string) error {
	data, err := storage.Read(stage, registrygen.ConfigName, 1<<20)
	if err != nil {
		return err
	}
	var config registrygen.Config
	if err = json.Unmarshal(data, &config); err != nil {
		return err
	}
	config.Registry.ID = id
	config.Registry.Name = name
	config.Registry.Description = ""
	config.Source = nil
	return storage.JSON(filepath.Join(stage, registrygen.ConfigName), config)
}
