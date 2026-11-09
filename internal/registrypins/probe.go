package registrypins

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

const (
	skillFile     = "SKILL.md"
	skillSuffix   = "/" + skillFile
	gitTimeout    = 4 * time.Minute
	maxTreeFiles  = 1_000_000
	githubTimeout = 20 * time.Second
)

var (
	safeSkill   = regexp.MustCompile(`^[A-Za-z0-9._@+()-]+(/[A-Za-z0-9._@+()-]+)*$`)
	githubRepo  = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(\.git)?/?$`)
	githubCheck = &http.Client{
		Timeout:       githubTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
)

// GitProber observes repositories through hardened, blobless Git fetches.
type GitProber struct {
	// Client performs the probes; nil uses a Hardened client under TempDir.
	Client  *gitclient.Client
	TempDir string
	// RepoMissing confirms a repository is gone; it defaults to a GitHub lookup.
	RepoMissing func(ctx context.Context, uri string) bool
}

func (g GitProber) client() *gitclient.Client {
	if g.Client != nil {
		return g.Client
	}
	return gitclient.New(gitclient.Options{
		Policy: gitclient.Hardened(), Credentials: gitclient.SharedCredentials(), TempDir: g.TempDir,
		Limits: gitclient.Limits{MaxFiles: maxTreeFiles},
	})
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

// gitSession reads trees from one temporary blobless repository.
type gitSession struct {
	repo *gitclient.Repo
}

func (g gitSession) skillFiles(ctx context.Context, commit string) ([]string, bool) {
	paths, err := g.repo.ListFiles(ctx, commit)
	if err != nil {
		return nil, false
	}
	files := []string{}
	for _, path := range paths {
		if path == skillFile || strings.HasSuffix(path, skillSuffix) {
			files = append(files, path)
		}
	}
	return files, true
}

func (g gitSession) fetch(ctx context.Context, commit string) error {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	_, err := g.repo.FetchBlobless(ctx, commit)
	return err
}

func (g gitSession) tree(ctx context.Context, commit, path string) (string, bool) {
	hash, err := g.repo.TreeHash(ctx, commit, skillDir(path))
	return hash, err == nil
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
	client := g.client()
	listCtx, cancel := context.WithTimeout(ctx, gitTimeout)
	remote, err := client.ResolveRemote(listCtx, uri, "")
	cancel()
	if err != nil {
		setAll(probes, pins, Probe{Repo: g.failedListing(ctx, uri, err)})
		return probes
	}
	head := remote.Commit
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
	repo, err := client.Temporary(ctx, uri, gitclient.TemporaryOptions{})
	if err != nil {
		setAll(probes, inspect, Probe{})
		return probes
	}
	defer func() { _ = repo.Close() }()
	session := gitSession{repo: repo}
	files, ok := session.prepare(ctx, head)
	if !ok {
		setAll(probes, inspect, Probe{})
		return probes
	}
	session.inspect(ctx, repositoryName(uri), head, files, inspect, probes)
	return probes
}

// failedListing reports RepoGone only when the host confirms the repository is
// missing, and asks only after a not-found or access failure: a host hides a
// private repository behind either, so neither alone proves it was deleted.
// Anything else, such as an unreachable host, stays a retryable RepoError.
func (g GitProber) failedListing(ctx context.Context, uri string, err error) RepoStatus {
	switch gitclient.KindOf(err) {
	case gitclient.KindMissingRepository, gitclient.KindAuth, gitclient.KindPermission:
	default:
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

func (g gitSession) prepare(ctx context.Context, head string) ([]string, bool) {
	if err := g.fetch(ctx, head); err != nil {
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
	if err := g.fetch(ctx, commit); err != nil {
		kind := gitclient.KindOf(err)
		return pinnedCommit{gone: kind == gitclient.KindMissingRef || kind == gitclient.KindMissingRepository}
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
