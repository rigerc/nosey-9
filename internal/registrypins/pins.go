// Package registrypins keeps the commit pins of a custom registry's skills
// current and notices upstream skills that have disappeared.
package registrypins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/jsonobject"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	// StateName is the probe history kept at a registry root.
	StateName = "nosey.pins.json"
	// DefaultTimeout bounds one update; repositories not reached wait for the next run.
	DefaultTimeout = time.Hour

	officialID       = "nosey:official"
	dateLayout       = "2006-01-02"
	requiredMisses   = 2
	minStrikeGapDays = 5
	maxQuarantine    = 25
	maxQuarantinePct = 5

	reasonRepoGone    = "repo_gone"
	reasonSkillAbsent = "skill_absent"
	reasonUnreachable = "unreachable"
)

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Pin is one skill locked to an upstream commit.
type Pin struct {
	ID, URI, Commit, SkillName string
	Quarantined                bool
}

// Quarantine mirrors the skill field written when an upstream is gone.
type Quarantine struct {
	Reason      string `json:"reason"`
	Since       string `json:"since"`
	LastSeenRef string `json:"lastSeenRef"`
	LastChecked string `json:"lastChecked,omitempty"`
}

// Strike records repeated misses of one skill.
type Strike struct {
	Misses      int    `json:"misses"`
	FirstMissAt string `json:"firstMissAt"`
	LastMissAt  string `json:"lastMissAt"`
	Reason      string `json:"reason"`
}

// State is the committed history a later run needs. SeenRepositories separates
// a repository that was deleted from one this runner could never read, such as
// a private repository, which looks the same to an anonymous probe.
type State struct {
	SchemaVersion    int               `json:"schemaVersion"`
	SeenRepositories []string          `json:"seenRepositories"`
	Probes           map[string]Strike `json:"probes"`
}

// RepoStatus is what a probe learned about a repository.
type RepoStatus int

// Repository outcomes. Only RepoGone can count as a miss.
const (
	RepoError RepoStatus = iota
	RepoOK
	RepoGone
)

// Probe is one skill's upstream observation.
type Probe struct {
	Repo RepoStatus
	Head string
	// Inspected is true when HeadHits was measured at Head.
	Inspected bool
	HeadHits  int
	// SameTree compares the skill directory at Head with the pinned commit; nil is unknown.
	SameTree *bool
	// PinGone is true when the pinned commit definitely can no longer be fetched.
	PinGone bool
}

// Prober observes one repository for its pins. It never fails: problems are
// reported as RepoError so they cannot be mistaken for a missing skill.
type Prober interface {
	Probe(ctx context.Context, uri string, pins []Pin) map[string]Probe
}

// Options controls one update.
type Options struct {
	// Enforce writes quarantine into the skill data instead of only reporting it.
	Enforce bool
	Timeout time.Duration
	Prober  Prober
	// Today overrides the calendar date, as YYYY-MM-DD.
	Today string
}

// Summary describes one update for review.
type Summary struct {
	Pinned, Probed                                                   int
	Bumped, Restored, Quarantined, WouldQuarantine, Strikes, Skipped []string
	Unreachable                                                      []string
	RailTripped, TimedOut, Enforced                                  bool
}

// Result reports one update.
type Result struct {
	Summary Summary
	Changed []string
}

// CheckResult reports the offline consistency of a registry's pins.
type CheckResult struct {
	Pinned, Unpinned, Quarantined, Open int
	Problems                            []string
}

type skillRecord struct {
	raw        json.RawMessage
	id         string
	pin        *Pin
	quarantine *Quarantine
}

type registryData struct {
	root      string
	cfg       registrygen.Config
	version   int
	skills    []skillRecord
	stateData []byte
	state     State
}

func load(root string) (*registryData, error) {
	target, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cfg, err := registrygen.Load(target)
	if err != nil {
		return nil, err
	}
	data := &registryData{root: target, cfg: cfg}
	raw, err := storage.Read(target, cfg.Files["skills"], 16<<20)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Version int               `json:"schemaVersion"`
		Skills  []json.RawMessage `json:"skills"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Files["skills"], err)
	}
	data.version = doc.Version
	for _, entity := range doc.Skills {
		record, parseErr := parseSkill(entity)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", cfg.Files["skills"], parseErr)
		}
		data.skills = append(data.skills, record)
	}
	return data, data.loadState()
}

func parseSkill(entity json.RawMessage) (skillRecord, error) {
	var skill struct {
		ID     string `json:"id"`
		Source *struct {
			Kind      string `json:"kind"`
			URI       string `json:"uri"`
			Ref       string `json:"ref"`
			SkillName string `json:"skillName"`
		} `json:"source"`
		Quarantine *Quarantine `json:"quarantine"`
	}
	if err := json.Unmarshal(entity, &skill); err != nil {
		return skillRecord{}, err
	}
	record := skillRecord{raw: entity, id: skill.ID, quarantine: skill.Quarantine}
	source := skill.Source
	if source != nil && source.Kind == "git" && source.SkillName != "" && fullCommit.MatchString(source.Ref) &&
		gitpolicy.Validate(source.URI, source.Ref) == nil {
		record.pin = &Pin{ID: skill.ID, URI: expandURI(source.URI), Commit: source.Ref, SkillName: source.SkillName, Quarantined: skill.Quarantine != nil}
	}
	return record, nil
}

// expandURI turns the OWNER/NAME shorthand into the GitHub URL it stands for.
func expandURI(uri string) string {
	if strings.Contains(uri, "://") || strings.HasPrefix(uri, "git@") {
		return uri
	}
	return "https://github.com/" + uri
}

func repoKey(uri string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(uri, "/"), ".git"))
}

func (d *registryData) loadState() error {
	d.state = State{SchemaVersion: 1, SeenRepositories: []string{}, Probes: map[string]Strike{}}
	raw, err := storage.Read(d.root, StateName, 8<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	d.stateData = raw
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&d.state); err != nil {
		return fmt.Errorf("%s: %w", StateName, err)
	}
	if d.state.SchemaVersion != 1 {
		return fmt.Errorf("%s: unsupported schemaVersion %d", StateName, d.state.SchemaVersion)
	}
	if d.state.Probes == nil {
		d.state.Probes = map[string]Strike{}
	}
	if d.state.SeenRepositories == nil {
		d.state.SeenRepositories = []string{}
	}
	return nil
}

// Check reports pin state without using the network or writing.
func Check(root string) (CheckResult, error) {
	data, err := load(root)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{Open: len(data.state.Probes), Problems: []string{}}
	for _, skill := range data.skills {
		if skill.pin != nil {
			result.Pinned++
		} else {
			result.Unpinned++
		}
		if skill.quarantine == nil {
			continue
		}
		result.Quarantined++
		strike, ok := data.state.Probes[skill.id]
		if !ok || strike.Misses < requiredMisses || strike.Reason != skill.quarantine.Reason {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: quarantine has no matching probe history (needs %d misses with the same reason)", skill.id, requiredMisses))
		}
	}
	return result, nil
}

type group struct {
	key, uri string
	pins     []Pin
}

func (d *registryData) groups() []group {
	byKey := map[string]*group{}
	for _, skill := range d.skills {
		if skill.pin == nil {
			continue
		}
		key := repoKey(skill.pin.URI)
		if byKey[key] == nil {
			byKey[key] = &group{key: key, uri: skill.pin.URI}
		}
		byKey[key].pins = append(byKey[key].pins, *skill.pin)
	}
	groups := make([]group, 0, len(byKey))
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		groups = append(groups, *byKey[key])
	}
	return groups
}

func (d *registryData) probe(ctx context.Context, prober Prober, timeout time.Duration) (decisions map[string]decision, seen map[string]bool, timedOut bool) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	seen = map[string]bool{}
	for _, key := range d.state.SeenRepositories {
		seen[key] = true
	}
	decisions = map[string]decision{}
	for _, repository := range d.groups() {
		if probeCtx.Err() != nil {
			return decisions, seen, true
		}
		probes := prober.Probe(probeCtx, repository.uri, repository.pins)
		for _, pin := range repository.pins {
			probe := probes[pin.ID]
			if probe.Repo == RepoOK {
				seen[repository.key] = true
			}
			if probe.Repo == RepoGone && !seen[repository.key] {
				decisions[pin.ID] = decision{action: actionSkip, reason: reasonUnreachable}
				continue
			}
			decisions[pin.ID] = decide(pin, probe)
		}
	}
	return decisions, seen, false
}

// Update probes every pinned repository, then re-pins changed skills, records
// misses and, when enforced, quarantines skills that stayed missing.
func Update(ctx context.Context, root string, opts Options) (Result, error) {
	data, err := load(root)
	if err != nil {
		return Result{}, err
	}
	if data.cfg.Registry.ID == officialID {
		return Result{}, fmt.Errorf("%s is maintained by its own pin workflow", officialID)
	}
	if opts.Today == "" {
		opts.Today = time.Now().UTC().Format(dateLayout)
	}
	if opts.Prober == nil {
		opts.Prober = GitProber{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	decisions, seen, timedOut := data.probe(ctx, opts.Prober, opts.Timeout)
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	edits, strikes, summary := apply(data.skills, data.state.Probes, decisions, opts.Today, opts.Enforce)
	summary.TimedOut = timedOut
	summary.Enforced = opts.Enforce
	data.state.Probes = strikes
	data.state.SeenRepositories = append([]string{}, slices.Sorted(maps.Keys(seen))...)
	result := Result{Summary: summary, Changed: []string{}}
	if len(edits) > 0 {
		rendered, renderErr := data.render(edits)
		if renderErr != nil {
			return result, renderErr
		}
		generated, genErr := registrygen.Run(data.root, registrygen.Options{Command: "pins", Overrides: map[string][]byte{"skills": rendered}})
		if genErr != nil {
			return result, genErr
		}
		result.Changed = append(result.Changed, generated.ChangedFiles...)
	}
	changed, err := data.writeState()
	if err != nil {
		return result, err
	}
	if changed {
		result.Changed = append(result.Changed, StateName)
	}
	return result, nil
}

func (d *registryData) writeState() (bool, error) {
	if d.stateData == nil && len(d.state.Probes) == 0 && len(d.state.SeenRepositories) == 0 {
		return false, nil
	}
	encoded, err := json.MarshalIndent(d.state, "", "  ")
	if err != nil {
		return false, err
	}
	encoded = append(encoded, '\n')
	if bytes.Equal(encoded, d.stateData) {
		return false, nil
	}
	return true, storage.WriteAt(d.root, StateName, encoded)
}

type edit struct {
	ref             string
	quarantine      *Quarantine
	clearQuarantine bool
}

// render rewrites only the edited skills and keeps every field's order. The
// generator canonicalizes the result.
func (d *registryData) render(edits map[string]edit) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"schemaVersion":` + strconv.Itoa(d.version) + `,"skills":[`)
	for i, skill := range d.skills {
		if i > 0 {
			b.WriteByte(',')
		}
		raw := skill.raw
		if change, ok := edits[skill.id]; ok {
			patched, err := patchSkill(raw, change)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", skill.id, err)
			}
			raw = patched
		}
		b.Write(raw)
	}
	b.WriteString("]}")
	return b.Bytes(), nil
}

func patchSkill(raw json.RawMessage, change edit) (json.RawMessage, error) {
	skill, err := jsonobject.Parse(raw)
	if err != nil {
		return nil, err
	}
	if change.ref != "" {
		if err = setSourceRef(skill, change.ref); err != nil {
			return nil, err
		}
	}
	if change.clearQuarantine {
		skill.Remove("quarantine")
	}
	if change.quarantine != nil {
		value, marshalErr := json.Marshal(change.quarantine)
		if marshalErr != nil {
			return nil, marshalErr
		}
		skill.Set("quarantine", value)
	}
	return skill.Marshal()
}

func setSourceRef(skill *jsonobject.Object, commit string) error {
	raw, _ := skill.Get("source")
	source, err := jsonobject.Parse(raw)
	if err != nil {
		return err
	}
	ref, err := json.Marshal(commit)
	if err != nil {
		return err
	}
	source.Set("ref", ref)
	updated, err := source.Marshal()
	if err != nil {
		return err
	}
	skill.Set("source", updated)
	return nil
}
