package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/githubclient"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	locationsPath   = "assessments/locations.json"
	maxFailureBytes = 1500
)

// GitState summarizes the registry checkout.
type GitState struct {
	Repository  bool   `json:"repository"`
	Publishable bool   `json:"publishable"`
	Commit      string `json:"commit"`
	Branch      string `json:"branch"`
	Remote      string `json:"remote"`
	// unsupported records a SHA-256 or reftable repository.
	unsupported bool
}

// gitClient builds a client for the user's own origin. Each operation gets its
// own temporary parent; construction performs no I/O.
func (s *Server) gitClient(tempDir string) *gitclient.Client {
	policy := gitclient.UserOrigin()
	if s.opts.GitPolicy != nil {
		policy = *s.opts.GitPolicy
	}
	return gitclient.New(gitclient.Options{Policy: policy, Credentials: s.opts.Credentials, TempDir: tempDir})
}

// upstreamClient builds a client for repositories named in a draft.
func (s *Server) upstreamClient() *gitclient.Client {
	policy := gitclient.Hardened()
	if s.opts.GitPolicy != nil {
		policy = *s.opts.GitPolicy
	}
	return gitclient.New(gitclient.Options{Policy: policy, Credentials: s.opts.Credentials, TempDir: s.opts.TempDir})
}

// gitState reads the checkout. The repository root must equal the registry root,
// so a registry stored inside some other project is not treated as a checkout.
func (s *Server) gitState(context.Context) GitState {
	state := GitState{}
	repo, err := s.gitClient(s.opts.TempDir).OpenUser(s.root)
	if err != nil {
		state.unsupported = gitclient.KindOf(err) == gitclient.KindUnsupportedFormat
		return state
	}
	resolved, err := filepath.EvalSymlinks(repo.Dir())
	if err != nil || resolved != s.root {
		return state
	}
	state.Repository = true
	state.Commit, _ = repo.Head()
	if state.Commit != "" {
		state.Branch, _ = repo.Branch()
	}
	state.Remote, _ = repo.OriginURL()
	state.Publishable = state.Remote != ""
	return state
}

// finalize writes ops into dir and runs everything CI would run on the result.
func (s *Server) finalize(ctx context.Context, dir string, ops []Op) ([]Conflict, error) {
	cfg, err := registrygen.Load(dir)
	if err != nil {
		return nil, err
	}
	docs, err := loadDocuments(dir, cfg)
	if err != nil {
		return nil, err
	}
	conflicts, err := replay(ops, docs, s.automated)
	if err != nil || len(conflicts) > 0 {
		return conflicts, err
	}
	data, err := overrides(docs)
	if err != nil {
		return nil, err
	}
	if _, err = registrygen.Run(dir, registrygen.Options{Overrides: data, Command: "generate"}); err != nil {
		return nil, err
	}
	if s.automated {
		if err = recordLocations(dir, ops); err != nil {
			return nil, err
		}
	}
	if s.automated && s.opts.RegistryChecks {
		if err = s.runRegistryScripts(ctx, dir, ops); err != nil {
			return nil, err
		}
	}
	return nil, s.checkEffective(ctx, dir)
}

// locationsFile mirrors the document locate.mjs writes.
type locationsFile struct {
	SchemaVersion int                 `json:"schema_version"`
	RegistryID    string              `json:"registry_id"`
	Locations     map[string]Location `json:"locations"`
}

// recordLocations adds new skills to assessments/locations.json, formatted as
// the registry's JSON.stringify writer does so the diff stays minimal.
func recordLocations(dir string, ops []Op) error {
	pending := []Op{}
	for _, op := range ops {
		if op.Location != nil && op.Entity != nil {
			pending = append(pending, op)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	data, err := storage.Read(dir, locationsPath, maxDocumentBytes)
	if err != nil {
		return err
	}
	var document locationsFile
	if err = json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%s: %w", locationsPath, err)
	}
	if document.Locations == nil {
		document.Locations = map[string]Location{}
	}
	for _, op := range pending {
		document.Locations[op.ID] = *op.Location
	}
	encoded, err := encodeLocations(document)
	if err != nil {
		return err
	}
	return storage.Write(filepath.Join(dir, locationsPath), encoded)
}

func encodeLocations(document locationsFile) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// scriptFailure keeps the end of a script's output, where failures are reported.
func scriptFailure(output process.Output) string {
	detail := strings.TrimSpace(string(output.Stderr) + "\n" + string(output.Stdout))
	if len(detail) > maxFailureBytes {
		detail = "…" + detail[len(detail)-maxFailureBytes:]
	}
	return storage.Safe(detail)
}

// runRegistryScripts applies the registry's own projection and checks in CI
// order. These scripts come from the checkout, hence the explicit opt-in.
func (s *Server) runRegistryScripts(ctx context.Context, dir string, ops []Op) error {
	run := func(name string, args ...string) error {
		output, err := s.opts.Runner.Run(ctx, dir, nil, name, args...)
		if err != nil {
			command := name + " " + strings.Join(args, " ")
			if name == "node" && len(args) > 0 && args[0] == "--test" {
				command = "node --test"
			}
			return fmt.Errorf("%s: %s", command, scriptFailure(output))
		}
		return nil
	}
	for _, op := range ops {
		if op.Role == roleSkills && op.Entity == nil {
			empty, err := os.MkdirTemp("", "nosey-studio-scores-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(empty) }()
			if err = run("node", "scripts/jev.mjs", "merge", "--dir", empty); err != nil {
				return err
			}
			break
		}
	}
	if err := run("node", "scripts/sync.mjs", "--write"); err != nil {
		return err
	}
	if _, err := registrygen.Run(dir, registrygen.Options{Command: "generate"}); err != nil {
		return err
	}
	if err := run("npm", "run", "--silent", "check"); err != nil {
		return err
	}
	tests, err := filepath.Glob(filepath.Join(dir, "tests", "*.test.mjs"))
	if err != nil {
		return err
	}
	if len(tests) == 0 {
		return nil
	}
	return run("node", append([]string{"--test"}, tests...)...)
}

func (s *Server) checkEffective(ctx context.Context, dir string) error {
	catalog, err := s.effective(ctx, dir)
	if err != nil {
		return err
	}
	return firstError(catalog.Diagnostics)
}

func firstError(diagnostics []domain.Diagnostic) error {
	for i := range diagnostics {
		if diagnostics[i].Severity == "error" {
			return fmt.Errorf("%s: %s (%s)", diagnostics[i].Code, diagnostics[i].Message, diagnostics[i].Subject)
		}
	}
	return nil
}

type conflictResult struct {
	Conflicts []Conflict `json:"conflicts"`
}

func (s *Server) recordConflicts(conflicts []Conflict) (response, error) {
	s.conflicts = make(map[string]Conflict, len(conflicts))
	for _, conflict := range conflicts {
		s.conflicts[Op{Role: conflict.Role, ID: conflict.ID}.key()] = conflict
	}
	return encoded(http.StatusConflict, conflictResult{conflicts})
}

func (s *Server) requireClean(ctx context.Context) error {
	if len(s.draft.Ops) == 0 {
		return badRequest("there are no staged changes")
	}
	if err := firstError(s.diagnostics(ctx, s.draft.Ops)); err != nil {
		return badRequest("fix validation errors first: %v", err)
	}
	return nil
}

// handleApply writes the draft into the working tree.
func (s *Server) handleApply(r *http.Request) (response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireClean(r.Context()); err != nil {
		return response{}, err
	}
	// Registry scripts can fail after files are rewritten, so rehearse on a copy.
	if s.automated && s.opts.RegistryChecks {
		rehearsal, err := os.MkdirTemp("", "nosey-studio-apply-")
		if err != nil {
			return response{}, err
		}
		defer func() { _ = os.RemoveAll(rehearsal) }()
		if err = copyTree(s.root, rehearsal); err != nil {
			return response{}, err
		}
		conflicts, err := s.finalize(r.Context(), rehearsal, s.draft.Ops)
		if err != nil {
			return response{}, badRequest("%v", err)
		}
		if len(conflicts) > 0 {
			return s.recordConflicts(conflicts)
		}
	}
	conflicts, err := s.finalize(r.Context(), s.root, s.draft.Ops)
	if err != nil {
		return response{}, badRequest("%v", err)
	}
	if len(conflicts) > 0 {
		return s.recordConflicts(conflicts)
	}
	count := len(s.draft.Ops)
	s.draft = Draft{Version: 1, Ops: []Op{}}
	if err = saveDraft(s.opts.Store.Data, s.root, s.draft); err != nil {
		return response{}, err
	}
	// The applied changes exist only locally, so a remote view would hide them.
	if s.source.Mode == sourceRemote {
		s.useCheckout("Showing your working tree, which now holds the applied changes. Refresh to return to the remote.")
	}
	return ok(map[string]int{"applied": count})
}

// copyTree copies a registry without its Git metadata or dependencies.
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name := entry.Name()
		if relative != "." && (name == ".git" || name == "node_modules") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		data, err := storage.Read(source, relative, maxDocumentBytes*2)
		if err != nil {
			return err
		}
		return storage.Write(filepath.Join(target, relative), data)
	})
}

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

type publishResult struct {
	Published Published `json:"published"`
	Updated   bool      `json:"updated"`
}

// handlePublish replays the draft on the remote default branch in a private
// temporary repository, so the user's checkout (often a pinned submodule) is
// never touched.
func (s *Server) handlePublish(r *http.Request) (response, error) {
	var input struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeInto(r, &input); err != nil {
		return response{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return response{}, badRequest("a title is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := r.Context()
	if err := s.requireClean(ctx); err != nil {
		return response{}, err
	}
	git := s.gitState(ctx)
	if git.unsupported {
		return response{}, badRequest("this checkout uses SHA-256 or reftable Git storage, which the studio cannot publish from")
	}
	if !git.Publishable {
		return response{}, badRequest("publishing needs a Git checkout with an origin remote; use Apply to working tree")
	}
	user, err := s.gitClient(s.opts.TempDir).OpenUser(s.root)
	if err != nil {
		return response{}, badRequest("%s", storage.Safe(err.Error()))
	}
	// Commit identity lives in the user's config, not the temporary repository's.
	identity, err := user.Identity()
	if err != nil {
		return response{}, badRequest("publishing needs a commit identity; set user.name and user.email with git config")
	}
	base := defaultBranch(user)
	temporary, err := user.TemporaryOrigin(ctx, gitclient.TemporaryOptions{Worktree: true})
	if err != nil {
		return response{}, publishSetupError(err)
	}
	defer func() { _ = temporary.Close() }()
	commit, err := temporary.FetchRef(ctx, "refs/heads/"+base)
	if err != nil {
		return response{}, badRequest("could not fetch origin/%s: %s", base, storage.Safe(err.Error()))
	}
	if err = temporary.PrepareWorktree(ctx, commit); err != nil {
		return response{}, badRequest("%s", storage.Safe(err.Error()))
	}
	conflicts, err := s.finalize(ctx, temporary.Dir(), s.draft.Ops)
	if err != nil {
		return response{}, badRequest("%v", err)
	}
	if len(conflicts) > 0 {
		return s.recordConflicts(conflicts)
	}
	result, err := s.pushBranch(ctx, user, temporary, publishRequest{base: base, title: title, body: input.Body, identity: identity})
	if result.Published.Branch != "" {
		s.draft.Published = &result.Published
		if saveErr := saveDraft(s.opts.Store.Data, s.root, s.draft); saveErr != nil {
			if err != nil {
				return response{}, badRequest("%s (and the published branch could not be recorded: %v)", storage.Safe(err.Error()), saveErr)
			}
			return response{}, saveErr
		}
	}
	if err != nil {
		return response{}, badRequest("%s", storage.Safe(err.Error()))
	}
	return ok(result)
}

// publishSetupError explains origins the native transport refuses.
func publishSetupError(err error) error {
	if gitclient.KindOf(err) == gitclient.KindInvalidSource {
		return badRequest("publishing needs an https or ssh origin; local path and file remotes are not supported (%s)", storage.Safe(err.Error()))
	}
	return badRequest("%s", storage.Safe(err.Error()))
}

// defaultBranch follows the checkout's origin/HEAD, falling back to main.
func defaultBranch(user *gitclient.Repo) string {
	head, err := user.OriginHead()
	if err != nil || head == "" {
		return "main"
	}
	return strings.TrimPrefix(head, "origin/")
}

type publishRequest struct {
	base, title, body string
	identity          gitclient.Identity
}

// pushBranch commits the worktree and opens, or lease-updates, the pull request.
// A returned Published with a Branch is persisted even when err is set: the
// branch exists on the remote and a retry must update it.
func (s *Server) pushBranch(ctx context.Context, user, repo *gitclient.Repo, request publishRequest) (publishResult, error) {
	paths := []string{registrygen.ConfigName, registrygen.LockName, s.cfg.Manifest}
	for _, path := range s.cfg.Files {
		paths = append(paths, path)
	}
	if _, err := os.Stat(filepath.Join(repo.Dir(), "assessments")); err == nil {
		paths = append(paths, "assessments")
	}
	commit, err := repo.Commit(ctx, gitclient.CommitOptions{Message: request.title, Paths: paths, Identity: request.identity})
	if gitclient.KindOf(err) == gitclient.KindNoChanges {
		return publishResult{}, errors.New("the draft produces no change against the remote branch")
	}
	if err != nil {
		return publishResult{}, err
	}
	if previous := s.draft.Published; previous != nil && branchPattern.MatchString(previous.Branch) {
		return s.updateBranch(ctx, user, repo, *previous, commit, request)
	}
	branch := "studio/" + time.Now().UTC().Format("20060102-150405")
	if err = repo.Push(ctx, commit, branch); err != nil {
		return publishResult{}, pushError(err)
	}
	return s.withPullRequest(ctx, user, Published{Branch: branch, Commit: commit, Base: request.base}, false, request)
}

// updateBranch advances an already-published branch only if the remote is still
// where it was left, opening the pull request if an earlier attempt could not.
func (s *Server) updateBranch(ctx context.Context, user, repo *gitclient.Repo, previous Published, commit string, request publishRequest) (publishResult, error) {
	if err := repo.PushLease(ctx, commit, previous.Branch, previous.Commit); err != nil {
		if gitclient.KindOf(err) == gitclient.KindLeaseConflict {
			return publishResult{}, fmt.Errorf("branch %s changed on the remote since it was last published, so it was not overwritten; review it there, or clear the draft to publish on a new branch", previous.Branch)
		}
		return publishResult{}, pushError(err)
	}
	updated := previous
	updated.Commit = commit
	if updated.URL != "" {
		return publishResult{Published: updated, Updated: true}, nil
	}
	return s.withPullRequest(ctx, user, updated, true, request)
}

func pushError(err error) error {
	switch gitclient.KindOf(err) {
	case gitclient.KindAuth, gitclient.KindPermission:
		return fmt.Errorf("could not push to origin: %w; %s", err, tokenHint)
	}
	return fmt.Errorf("could not push to origin: %w", err)
}

const tokenHint = "a GitHub token with write access is required: set GITHUB_TOKEN (GH_ENTERPRISE_TOKEN with GH_HOST on Enterprise) or run gh auth login" //nolint:gosec // an instructional message that holds no credential

// withPullRequest opens the pull request for published. When that fails the
// pushed branch is kept, the result still names it, and the error says so.
func (s *Server) withPullRequest(ctx context.Context, user *gitclient.Repo, published Published, updated bool, request publishRequest) (publishResult, error) {
	url, err := s.openPullRequest(ctx, user, published.Branch, request)
	result := publishResult{Published: published, Updated: updated}
	if err != nil {
		detail := storage.Safe(err.Error())
		if errors.Is(err, gitclient.KindAuth) || errors.Is(err, gitclient.KindPermission) {
			detail += "; " + tokenHint
		}
		return result, fmt.Errorf("branch was pushed but a pull request could not be opened (branch %s): %s", published.Branch, detail)
	}
	result.Published.URL = url
	return result, nil
}

// openPullRequest looks for an open pull request first, so a retry after a
// partial failure does not create a duplicate.
func (s *Server) openPullRequest(ctx context.Context, user *gitclient.Repo, branch string, request publishRequest) (string, error) {
	location, ok, err := user.OriginLocation()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("origin is not a repository on a GitHub host")
	}
	api, err := githubclient.New(ctx, githubclient.Options{Host: location.Host, Credentials: s.opts.Credentials, RequireAuth: true, BaseURL: s.opts.GitHubBaseURL})
	if err != nil {
		return "", err
	}
	existing, found, err := api.FindPullRequest(ctx, location.Owner, location.Name, branch, request.base)
	if err != nil {
		return "", err
	}
	if found {
		return existing.URL, nil
	}
	created, err := api.CreatePullRequest(ctx, githubclient.CreatePullRequestRequest{
		Owner: location.Owner, Repo: location.Name, Base: request.base, Head: branch, Title: request.title, Body: request.body,
	})
	if err != nil {
		return "", err
	}
	return created.URL, nil
}

// Resolution reports where a skill lives upstream and whether automation can track it.
type Resolution struct {
	Repository string          `json:"repository"`
	Commit     string          `json:"commit"`
	Skills     []ResolvedSkill `json:"skills"`
	Selected   *ResolvedSkill  `json:"selected,omitempty"`
}

// ResolvedSkill is one SKILL.md candidate at the resolved commit.
type ResolvedSkill struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Trackable   bool   `json:"trackable"`
	Reason      string `json:"reason,omitempty"`
	Frontmatter string `json:"frontmatterName,omitempty"`
	Description string `json:"description,omitempty"`
}

var safeSkillPath = regexp.MustCompile(`^[A-Za-z0-9._@+()-]+(?:/[A-Za-z0-9._@+()-]+)*$`)

// trackableSkills applies locate.mjs's rule: a skill is tracked only when exactly
// one SKILL.md sits in a directory named after it.
func trackableSkills(paths []string, repository string) []ResolvedSkill {
	counts := map[string]int{}
	candidates := make([]ResolvedSkill, 0, len(paths))
	for _, path := range paths {
		if !safeSkillPath.MatchString(path) || strings.Contains("/"+path+"/", "/../") {
			continue
		}
		name := repository
		if path != "SKILL.md" {
			name = filepath.Base(filepath.Dir(path))
		}
		counts[name]++
		candidates = append(candidates, ResolvedSkill{Name: name, Path: path})
	}
	for i := range candidates {
		candidates[i].Trackable = counts[candidates[i].Name] == 1
		if !candidates[i].Trackable {
			candidates[i].Reason = "more than one directory has this name, so automation cannot track it"
		}
	}
	return candidates
}

// frontmatterFields extracts name and description without a YAML dependency.
func frontmatterFields(content string) (string, string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	var name, description string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return name, description
}

// handleResolveSkill pins an upstream repository and lists its skills.
func (s *Server) handleResolveSkill(r *http.Request) (response, error) {
	var input struct {
		URI       string `json:"uri"`
		SkillName string `json:"skillName"`
	}
	if err := decodeInto(r, &input); err != nil {
		return response{}, err
	}
	uri := strings.TrimSuffix(strings.TrimSpace(input.URI), ".git")
	match := githubRepository.FindStringSubmatch(uri)
	if match == nil {
		return response{}, badRequest("enter a repository as https://github.com/OWNER/REPO")
	}
	ctx := r.Context()
	client := s.upstreamClient()
	head, err := client.ResolveRemote(ctx, uri, "HEAD")
	if err != nil {
		return response{}, badRequest("%s", storage.Safe(err.Error()))
	}
	if !fullCommit.MatchString(head.Commit) {
		return response{}, badRequest("could not resolve the repository HEAD")
	}
	repo, err := client.Temporary(ctx, uri, gitclient.TemporaryOptions{})
	if err != nil {
		return response{}, badRequest("%s", storage.Safe(err.Error()))
	}
	defer func() { _ = repo.Close() }()
	// Reading a selected skill's frontmatter needs its blob; a plain listing does not.
	fetch := repo.FetchBlobless
	if input.SkillName != "" {
		fetch = repo.FetchRef
	}
	commit, err := fetch(ctx, head.Commit)
	if err != nil {
		return response{}, badRequest("%s", storage.Safe(err.Error()))
	}
	files, err := repo.ListFiles(ctx, commit)
	if err != nil {
		return response{}, badRequest("could not list the pinned tree")
	}
	paths := []string{}
	for _, path := range files {
		if path == "SKILL.md" || strings.HasSuffix(path, "/SKILL.md") {
			paths = append(paths, path)
		}
	}
	result := Resolution{Repository: match[1] + "/" + match[2], Commit: commit, Skills: trackableSkills(paths, match[2])}
	if input.SkillName != "" {
		if err = selectSkill(ctx, repo, commit, input.SkillName, &result); err != nil {
			return response{}, err
		}
	}
	return ok(result)
}

func selectSkill(ctx context.Context, repo *gitclient.Repo, commit, name string, result *Resolution) error {
	for i := range result.Skills {
		if result.Skills[i].Name != name {
			continue
		}
		selected := result.Skills[i]
		if content, err := repo.ReadFile(ctx, commit, selected.Path, maxDocumentBytes); err == nil {
			selected.Frontmatter, selected.Description = frontmatterFields(string(content))
			if selected.Frontmatter != "" && selected.Frontmatter != name {
				selected.Trackable = false
				selected.Reason = "the frontmatter name " + selected.Frontmatter + " differs from the directory, so automation would quarantine it"
			}
		}
		result.Selected = &selected
		return nil
	}
	return badRequest("no SKILL.md named %s at this commit", name)
}
