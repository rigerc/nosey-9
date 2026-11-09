package registrypins

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/process"
)

const (
	skillFile     = "SKILL.md"
	skillSuffix   = "/" + skillFile
	gitTimeout    = 4 * time.Minute
	gitOutputSize = 32 << 20
	githubTimeout = 20 * time.Second
)

var (
	gitMissing  = regexp.MustCompile(`(?i)not our ref|couldn't find remote ref|repository not found|does not appear to be a git repository|\b404\b|unadvertised object|bad object`)
	gitAuth     = regexp.MustCompile(`(?i)could not read username|authentication failed|terminal prompts disabled|\b40[13]\b|permission denied`)
	safeSkill   = regexp.MustCompile(`^[A-Za-z0-9._@+()-]+(/[A-Za-z0-9._@+()-]+)*$`)
	githubRepo  = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(\.git)?/?$`)
	githubCheck = &http.Client{
		Timeout:       githubTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
)

// GitProber observes repositories through hardened, blobless Git fetches.
type GitProber struct {
	Runner  process.Executor
	TempDir string
	// Env is appended to the hardened environment.
	Env []string
	// RepoMissing confirms a repository is gone; it defaults to a GitHub lookup.
	RepoMissing func(ctx context.Context, uri string) bool
}

// repoMissing is true only for an explicit GitHub 404; other hosts cannot be confirmed.
func repoMissing(ctx context.Context, uri string) bool {
	match := githubRepo.FindStringSubmatch(uri)
	if match == nil {
		return false
	}
	address := "https://github.com/" + url.PathEscape(match[1]) + "/" + url.PathEscape(match[2])
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, address, http.NoBody)
	if err != nil {
		return false
	}
	response, err := githubCheck.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusNotFound
}

type gitSession struct {
	runner process.Executor
	dir    string
	env    []string
}

func (g gitSession) run(ctx context.Context, args ...string) (process.Output, error) {
	return g.runner.Run(ctx, g.dir, g.env, "git", gitpolicy.HardenedArgs(args...)...)
}

func (g gitSession) skillFiles(ctx context.Context, commit string) ([]string, bool) {
	out, err := g.run(ctx, "ls-tree", "-r", "--name-only", "-z", commit)
	if err != nil {
		return nil, false
	}
	files := []string{}
	for _, path := range strings.Split(string(out.Stdout), "\x00") {
		if path == skillFile || strings.HasSuffix(path, skillSuffix) {
			files = append(files, path)
		}
	}
	return files, true
}

func (g gitSession) fetch(ctx context.Context, commit string) (process.Output, error) {
	return g.run(ctx, "fetch", "-q", "--depth=1", "--filter=blob:none", "origin", commit)
}

func (g gitSession) tree(ctx context.Context, commit, path string) (string, bool) {
	spec := commit + "^{tree}"
	if dir := skillDir(path); dir != "" {
		spec = commit + ":" + dir
	}
	out, err := g.run(ctx, "rev-parse", spec)
	return strings.TrimSpace(string(out.Stdout)), err == nil
}

func skillDir(path string) string {
	if path == skillFile {
		return ""
	}
	return strings.TrimSuffix(path, skillSuffix)
}

// candidates lists SKILL.md files whose directory is named after the skill. A
// repository-root SKILL.md matches the repository's own name.
func candidates(files []string, skillName, repoName string) []string {
	hits := []string{}
	for _, path := range files {
		if !safeSkill.MatchString(path) || slices.Contains(strings.Split(path, "/"), "..") {
			continue
		}
		if path == skillFile {
			if strings.EqualFold(repoName, skillName) {
				hits = append(hits, path)
			}
			continue
		}
		parts := strings.Split(skillDir(path), "/")
		if parts[len(parts)-1] == skillName {
			hits = append(hits, path)
		}
	}
	return hits
}

func repositoryName(uri string) string {
	trimmed := strings.TrimSuffix(strings.TrimSuffix(uri, "/"), ".git")
	return trimmed[strings.LastIndexAny(trimmed, "/:")+1:]
}

func setAll(probes map[string]Probe, pins []Pin, probe Probe) {
	for _, pin := range pins {
		probes[pin.ID] = probe
	}
}

// Probe observes one repository. It never returns an error: every failure
// becomes RepoError for the pins it affects.
func (g GitProber) Probe(ctx context.Context, uri string, pins []Pin) map[string]Probe {
	probes := map[string]Probe{}
	dir, err := os.MkdirTemp(g.TempDir, "nosey-pins-")
	if err != nil {
		setAll(probes, pins, Probe{})
		return probes
	}
	defer func() { _ = os.RemoveAll(dir) }()
	runner := g.Runner
	if runner == nil {
		runner = process.Runner{Timeout: gitTimeout, Limit: gitOutputSize}
	}
	session := gitSession{runner: runner, dir: dir, env: append(gitpolicy.HardenedEnv(), g.Env...)}

	out, err := session.run(ctx, "ls-remote", uri, "HEAD")
	if err != nil {
		setAll(probes, pins, Probe{Repo: g.failedListing(ctx, uri, string(out.Stderr))})
		return probes
	}
	head, _, _ := strings.Cut(string(out.Stdout), "\t")
	if !fullCommit.MatchString(head) {
		setAll(probes, pins, Probe{})
		return probes
	}
	inspect := []Pin{}
	for _, pin := range pins {
		if pin.Commit != head || pin.Quarantined {
			inspect = append(inspect, pin)
		} else {
			probes[pin.ID] = Probe{Repo: RepoOK, Head: head}
		}
	}
	if len(inspect) == 0 {
		return probes
	}
	files, ok := session.prepare(ctx, uri, head)
	if !ok {
		setAll(probes, inspect, Probe{})
		return probes
	}
	session.inspect(ctx, repositoryName(uri), head, files, inspect, probes)
	return probes
}

func (g GitProber) failedListing(ctx context.Context, uri, stderr string) RepoStatus {
	if !gitMissing.MatchString(stderr) && !gitAuth.MatchString(stderr) {
		return RepoError
	}
	confirm := g.RepoMissing
	if confirm == nil {
		confirm = repoMissing
	}
	if confirm(ctx, uri) {
		return RepoGone
	}
	return RepoError
}

func (g gitSession) prepare(ctx context.Context, uri, head string) ([]string, bool) {
	if _, err := g.run(ctx, "init", "-q"); err != nil {
		return nil, false
	}
	if _, err := g.run(ctx, "remote", "add", "origin", uri); err != nil {
		return nil, false
	}
	if _, err := g.fetch(ctx, head); err != nil {
		return nil, false
	}
	return g.skillFiles(ctx, head)
}

type pinnedCommit struct {
	files  []string
	gone   bool
	loaded bool
}

func (g gitSession) loadPinned(ctx context.Context, commit string) pinnedCommit {
	out, err := g.fetch(ctx, commit)
	if err != nil {
		return pinnedCommit{gone: gitMissing.MatchString(string(out.Stderr))}
	}
	files, ok := g.skillFiles(ctx, commit)
	return pinnedCommit{files: files, loaded: ok}
}

func (g gitSession) inspect(ctx context.Context, repoName, head string, headFiles []string, pins []Pin, probes map[string]Probe) {
	commits := map[string]pinnedCommit{}
	for _, pin := range pins {
		if _, done := commits[pin.Commit]; !done {
			commits[pin.Commit] = g.loadPinned(ctx, pin.Commit)
		}
	}
	for _, pin := range pins {
		pinned := commits[pin.Commit]
		pinnedPath := ""
		if hits := candidates(pinned.files, pin.SkillName, repoName); len(hits) == 1 {
			pinnedPath = hits[0]
		}
		headHits := candidates(headFiles, pin.SkillName, repoName)
		// Several same-named skills: follow the one at the pinned path while it exists.
		if len(headHits) > 1 && slices.Contains(headHits, pinnedPath) {
			headHits = []string{pinnedPath}
		}
		probe := Probe{Repo: RepoOK, Head: head, Inspected: true, HeadHits: len(headHits), PinGone: pinned.gone}
		if len(headHits) == 1 && pinned.loaded && pinnedPath != "" {
			probe.SameTree = g.sameTree(ctx, head, headHits[0], pin.Commit, pinnedPath)
		}
		probes[pin.ID] = probe
	}
}

func (g gitSession) sameTree(ctx context.Context, head, headPath, commit, pinnedPath string) *bool {
	current, currentOK := g.tree(ctx, head, headPath)
	old, oldOK := g.tree(ctx, commit, pinnedPath)
	if !currentOK || !oldOK {
		return nil
	}
	same := current == old
	return &same
}
