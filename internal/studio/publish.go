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
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	locationsPath   = "assessments/locations.json"
	maxFailureBytes = 1500
)

var gitEnv = []string{"GIT_TERMINAL_PROMPT=0"}

// GitState summarizes the registry checkout.
type GitState struct {
	Repository  bool   `json:"repository"`
	Publishable bool   `json:"publishable"`
	Commit      string `json:"commit"`
	Branch      string `json:"branch"`
	Remote      string `json:"remote"`
}

func (s *Server) git(ctx context.Context, dir string, args ...string) (string, error) {
	output, err := s.opts.Runner.Run(ctx, dir, gitEnv, "git", args...)
	if err != nil {
		detail := strings.TrimSpace(string(output.Stderr))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], storage.Safe(detail))
	}
	return strings.TrimSpace(string(output.Stdout)), nil
}

func (s *Server) gitState(ctx context.Context) GitState {
	state := GitState{}
	top, err := s.git(ctx, s.root, "rev-parse", "--show-toplevel")
	if err != nil {
		return state
	}
	resolved, err := filepath.EvalSymlinks(top)
	if err != nil || resolved != s.root {
		return state
	}
	state.Repository = true
	state.Commit, _ = s.git(ctx, s.root, "rev-parse", "HEAD")
	state.Branch, _ = s.git(ctx, s.root, "rev-parse", "--abbrev-ref", "HEAD")
	state.Remote, _ = s.git(ctx, s.root, "remote", "get-url", "origin")
	state.Publishable = state.Remote != ""
	return state
}

// finalize writes ops into dir and runs everything CI would run on the result.
func (s *Server) finalize(ctx context.Context, dir string, ops []Op) ([]Conflict, error) {
	docs, err := loadDocuments(dir, s.cfg)
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

// handlePublish replays the draft on the remote default branch in a temporary
// worktree, so the user's checkout (often a pinned submodule) is never touched.
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
	if !s.gitState(ctx).Publishable {
		return response{}, badRequest("publishing needs a Git checkout with an origin remote; use Apply to working tree")
	}
	base := s.defaultBranch(ctx)
	if _, err := s.git(ctx, s.root, "fetch", "--no-tags", "origin", base); err != nil {
		return response{}, badRequest("%v", err)
	}
	parent, err := os.MkdirTemp("", "nosey-studio-publish-")
	if err != nil {
		return response{}, err
	}
	defer func() { _ = os.RemoveAll(parent) }()
	tree := filepath.Join(parent, "tree")
	if _, err = s.git(ctx, s.root, "worktree", "add", "--detach", tree, "FETCH_HEAD"); err != nil {
		return response{}, badRequest("%v", err)
	}
	defer func() {
		cleanup := context.WithoutCancel(ctx)
		_, _ = s.git(cleanup, s.root, "worktree", "remove", "--force", tree)
		_, _ = s.git(cleanup, s.root, "worktree", "prune")
	}()
	conflicts, err := s.finalize(ctx, tree, s.draft.Ops)
	if err != nil {
		return response{}, badRequest("%v", err)
	}
	if len(conflicts) > 0 {
		return s.recordConflicts(conflicts)
	}
	result, err := s.pushBranch(ctx, tree, base, title, input.Body)
	if err != nil {
		return response{}, badRequest("%v", err)
	}
	s.draft.Published = &result.Published
	if err = saveDraft(s.opts.Store.Data, s.root, s.draft); err != nil {
		return response{}, err
	}
	return ok(result)
}

func (s *Server) defaultBranch(ctx context.Context) string {
	head, err := s.git(ctx, s.root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "main"
	}
	return strings.TrimPrefix(head, "origin/")
}

// pushBranch commits the worktree and opens, or force-updates, the pull request.
func (s *Server) pushBranch(ctx context.Context, tree, base, title, body string) (publishResult, error) {
	paths := []string{registrygen.ConfigName, registrygen.LockName, s.cfg.Manifest}
	for _, path := range s.cfg.Files {
		paths = append(paths, path)
	}
	if _, err := os.Stat(filepath.Join(tree, "assessments")); err == nil {
		paths = append(paths, "assessments")
	}
	if _, err := s.git(ctx, tree, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return publishResult{}, err
	}
	staged, err := s.git(ctx, tree, "diff", "--cached", "--name-only")
	if err != nil {
		return publishResult{}, err
	}
	if staged == "" {
		return publishResult{}, errors.New("the draft produces no change against the remote branch")
	}
	if _, err = s.git(ctx, tree, "commit", "-q", "-m", title); err != nil {
		return publishResult{}, err
	}
	commit, err := s.git(ctx, tree, "rev-parse", "HEAD")
	if err != nil {
		return publishResult{}, err
	}
	previous := s.draft.Published
	if previous != nil && branchPattern.MatchString(previous.Branch) {
		lease := "--force-with-lease=refs/heads/" + previous.Branch + ":" + previous.Commit
		if _, err = s.git(ctx, tree, "push", lease, "origin", "HEAD:refs/heads/"+previous.Branch); err != nil {
			return publishResult{}, err
		}
		updated := *previous
		updated.Commit = commit
		return publishResult{Published: updated, Updated: true}, nil
	}
	branch := "studio/" + time.Now().UTC().Format("20060102-150405")
	if _, err = s.git(ctx, tree, "push", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return publishResult{}, err
	}
	output, err := s.opts.Runner.Run(ctx, tree, gitEnv, "gh", "pr", "create", "--base", base, "--head", branch, "--title", title, "--body", body)
	if err != nil {
		return publishResult{}, fmt.Errorf("branch %s was pushed but gh could not open a pull request: %s", branch, storage.Safe(strings.TrimSpace(string(output.Stderr))))
	}
	return publishResult{Published: Published{Branch: branch, Commit: commit, URL: strings.TrimSpace(string(output.Stdout)), Base: base}}, nil
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
	remote := s.opts.GitURL(uri)
	listing, err := s.git(ctx, "", "ls-remote", remote, "HEAD")
	if err != nil {
		return response{}, badRequest("%v", err)
	}
	commit, _, _ := strings.Cut(listing, "\t")
	if !fullCommit.MatchString(commit) {
		return response{}, badRequest("could not resolve the repository HEAD")
	}
	work, err := os.MkdirTemp("", "nosey-studio-resolve-")
	if err != nil {
		return response{}, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}, {"fetch", "-q", "--depth=1", "--filter=blob:none", "origin", commit}} {
		if _, err = s.git(ctx, work, args...); err != nil {
			return response{}, badRequest("%v", err)
		}
	}
	tree, err := s.opts.Runner.Run(ctx, work, gitEnv, "git", "ls-tree", "-r", "--name-only", "-z", commit)
	if err != nil {
		return response{}, badRequest("could not list the pinned tree")
	}
	paths := []string{}
	for _, path := range strings.Split(string(tree.Stdout), "\x00") {
		if path == "SKILL.md" || strings.HasSuffix(path, "/SKILL.md") {
			paths = append(paths, path)
		}
	}
	result := Resolution{Repository: match[1] + "/" + match[2], Commit: commit, Skills: trackableSkills(paths, match[2])}
	if input.SkillName != "" {
		if err = s.selectSkill(ctx, work, commit, input.SkillName, &result); err != nil {
			return response{}, err
		}
	}
	return ok(result)
}

func (s *Server) selectSkill(ctx context.Context, work, commit, name string, result *Resolution) error {
	for i := range result.Skills {
		if result.Skills[i].Name != name {
			continue
		}
		selected := result.Skills[i]
		if content, err := s.git(ctx, work, "show", commit+":"+selected.Path); err == nil {
			selected.Frontmatter, selected.Description = frontmatterFields(content)
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
