package studio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rigerc/nosey-9/internal/jsonobject"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	roleSkills       = "skills"
	roleTechnologies = "technologies"
	roleMappings     = "mappings"
	officialID       = "nosey:official"
	maxDocumentBytes = 16 << 20
)

var (
	// projectedFields are rewritten by the registry's assessment automation.
	projectedFields = []string{"quality", "score", "assessment"}
	// automationFields also include fields owned by the pin workflow and by
	// the skill's repository average.
	automationFields = []string{"quality", "score", "assessment", "quarantine", "source", "repositoryId"}
	githubRepository = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)$`)
	fullCommit       = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Op is one pending entity change. Base is the entity the edit started from;
// a nil Entity deletes and a nil Base creates.
type Op struct {
	Role   string          `json:"role"`
	ID     string          `json:"id"`
	Entity json.RawMessage `json:"entity"`
	Base   json.RawMessage `json:"base"`
	// Location records where a newly added skill's SKILL.md lives.
	Location *Location `json:"location,omitempty"`
}

// Location is one entry of the registry's assessments/locations.json.
type Location struct {
	Commit string `json:"commit"`
	Path   string `json:"path"`
}

func (o Op) key() string { return o.Role + "\x00" + o.ID }

// Published records the pull request a draft was last pushed to.
type Published struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	URL    string `json:"url"`
	Base   string `json:"base"`
}

// Draft is the persisted set of unapplied changes for one registry.
type Draft struct {
	Version   int        `json:"version"`
	Ops       []Op       `json:"ops"`
	Published *Published `json:"published,omitempty"`
}

// document is one registry data file keyed by entity id.
type document struct {
	version json.RawMessage
	order   []string
	byID    map[string]json.RawMessage
}

func parseDocument(role string, data []byte) (*document, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	var entities []json.RawMessage
	if err := json.Unmarshal(top[role], &entities); err != nil {
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	doc := &document{version: top["schemaVersion"], order: make([]string, 0, len(entities)), byID: make(map[string]json.RawMessage, len(entities))}
	for _, entity := range entities {
		var header struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(entity, &header); err != nil {
			return nil, fmt.Errorf("%s: %w", role, err)
		}
		doc.put(header.ID, entity)
	}
	return doc, nil
}

func (d *document) put(id string, entity json.RawMessage) {
	if _, exists := d.byID[id]; !exists {
		d.order = append(d.order, id)
	}
	d.byID[id] = entity
}

func (d *document) remove(id string) {
	if _, exists := d.byID[id]; !exists {
		return
	}
	delete(d.byID, id)
	for i, current := range d.order {
		if current == id {
			d.order = append(d.order[:i], d.order[i+1:]...)
			return
		}
	}
}

func (d *document) entities() []json.RawMessage {
	out := make([]json.RawMessage, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.byID[id])
	}
	return out
}

func (d *document) encode(role string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"schemaVersion":`)
	b.Write(d.version)
	fmt.Fprintf(&b, `,%q:[`, role)
	for i, entity := range d.entities() {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(entity)
	}
	b.WriteString("]}")
	return b.Bytes(), nil
}

// loadDocuments reads every declared data file beneath root.
func loadDocuments(root string, cfg registrygen.Config) (map[string]*document, error) {
	docs := make(map[string]*document, len(cfg.Files))
	for role, path := range cfg.Files {
		data, err := storage.Read(root, path, maxDocumentBytes)
		if err != nil {
			return nil, err
		}
		doc, err := parseDocument(role, data)
		if err != nil {
			return nil, err
		}
		docs[role] = doc
	}
	return docs, nil
}

// normalizeEntity applies the writer rules registry CI depends on: projected
// fields stay last so sync.mjs reproduces the file byte for byte, and array
// fields consumers read unconditionally are always present.
func normalizeEntity(role string, entity json.RawMessage) (json.RawMessage, error) {
	parsed, err := jsonobject.Parse(entity)
	if err != nil {
		return nil, err
	}
	switch role {
	case roleSkills:
		if _, ok := parsed.Get("technologies"); !ok {
			parsed.Set("technologies", json.RawMessage("[]"))
		}
		parsed.MoveLast(append(append([]string{}, projectedFields...), "quarantine")...)
	case roleTechnologies:
		if _, ok := parsed.Get("rules"); !ok {
			parsed.Set("rules", json.RawMessage("[]"))
		}
	case roleMappings:
		if _, ok := parsed.Get("recommendations"); !ok {
			parsed.Set("recommendations", json.RawMessage("[]"))
		}
	}
	return parsed.Marshal()
}

// curated returns the entity without automation-owned fields, so upstream pin
// and score churn never reads as a conflict with a curator's edit.
func curated(role string, automated bool, entity json.RawMessage) json.RawMessage {
	if !automated || role != roleSkills || entity == nil {
		return entity
	}
	parsed, err := jsonobject.Parse(entity)
	if err != nil {
		return entity
	}
	for _, name := range automationFields {
		parsed.Remove(name)
	}
	out, err := parsed.Marshal()
	if err != nil {
		return entity
	}
	return out
}

// mergeOwned takes each automation field from current unless the edit
// deliberately changed it away from its base.
func mergeOwned(entity, base, current json.RawMessage) (json.RawMessage, error) {
	edited, err := jsonobject.Parse(entity)
	if err != nil {
		return nil, err
	}
	original, err := jsonobject.Parse(base)
	if err != nil {
		return nil, err
	}
	latest, err := jsonobject.Parse(current)
	if err != nil {
		return nil, err
	}
	for _, name := range automationFields {
		editedValue, hasEdited := edited.Get(name)
		baseValue, hasBase := original.Get(name)
		if hasEdited != hasBase || hasEdited && !sameJSON(editedValue, baseValue) {
			continue
		}
		if value, ok := latest.Get(name); ok {
			edited.Set(name, value)
		} else {
			edited.Remove(name)
		}
	}
	marshaled, err := edited.Marshal()
	if err != nil {
		return nil, err
	}
	return normalizeEntity(roleSkills, marshaled)
}

// Conflict is an entity changed upstream since the edit started.
type Conflict struct {
	Role    string          `json:"role"`
	ID      string          `json:"id"`
	Reason  string          `json:"reason"`
	Current json.RawMessage `json:"current"`
}

// replay applies ops to docs with an entity-level three-way merge.
func replay(ops []Op, docs map[string]*document, automated bool) ([]Conflict, error) {
	conflicts := []Conflict{}
	for _, op := range ops {
		doc := docs[op.Role]
		if doc == nil {
			return nil, fmt.Errorf("registry does not declare %s", op.Role)
		}
		current, exists := doc.byID[op.ID]
		add := func(reason string) {
			conflicts = append(conflicts, Conflict{Role: op.Role, ID: op.ID, Reason: reason, Current: current})
		}
		baseCurated, currentCurated := curated(op.Role, automated, op.Base), curated(op.Role, automated, current)
		switch {
		case op.Base == nil && op.Entity != nil:
			if exists && !sameJSON(current, op.Entity) {
				add("already exists with different content")
				continue
			}
			doc.put(op.ID, op.Entity)
		case op.Entity == nil:
			if !exists {
				continue
			}
			if !sameJSON(baseCurated, currentCurated) {
				add("changed since this deletion was staged")
				continue
			}
			doc.remove(op.ID)
		default:
			if !exists {
				add("deleted since this edit started")
				continue
			}
			if !sameJSON(baseCurated, currentCurated) && !sameJSON(curated(op.Role, automated, op.Entity), currentCurated) {
				add("changed since this edit started")
				continue
			}
			entity := op.Entity
			if automated && op.Role == roleSkills {
				merged, err := mergeOwned(op.Entity, op.Base, current)
				if err != nil {
					return nil, err
				}
				entity = merged
			}
			doc.put(op.ID, entity)
		}
	}
	return conflicts, nil
}

// draftPath keeps drafts outside the registry so they never dirty its tree.
func draftPath(data, root string) (string, string) {
	return filepath.Join(data, "studio"), rootKey(root) + ".json"
}

func loadDraft(data, root string) (Draft, error) {
	draft := Draft{Version: 1, Ops: []Op{}}
	dir, name := draftPath(data, root)
	raw, err := storage.Read(dir, name, maxDocumentBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return draft, nil
	}
	if err != nil {
		return draft, err
	}
	if err = json.Unmarshal(raw, &draft); err != nil {
		return draft, fmt.Errorf("studio draft: %w", err)
	}
	if draft.Ops == nil {
		draft.Ops = []Op{}
	}
	// A JSON null decodes into RawMessage as the literal bytes "null".
	for i := range draft.Ops {
		if string(draft.Ops[i].Entity) == "null" {
			draft.Ops[i].Entity = nil
		}
		if string(draft.Ops[i].Base) == "null" {
			draft.Ops[i].Base = nil
		}
	}
	return draft, nil
}

func saveDraft(data, root string, draft Draft) error {
	dir, name := draftPath(data, root)
	sort.SliceStable(draft.Ops, func(a, b int) bool { return draft.Ops[a].key() < draft.Ops[b].key() })
	raw, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return storage.Write(filepath.Join(dir, name), raw)
}

// policyError is a change the registry's ownership rules forbid.
type policyError struct{ message string }

func (e policyError) Error() string { return e.message }

func denied(format string, args ...any) error {
	return policyError{fmt.Sprintf(format, args...)}
}

// guardSkill enforces automation ownership on the official registry.
func guardSkill(op Op, checks bool, openProbe bool) error {
	switch {
	case op.Entity == nil:
		if !checks {
			return denied("deleting a skill also rewrites its assessments; restart the studio with --registry-checks")
		}
		if openProbe {
			return denied("skill %s has open pin-probe history; let the pin workflow resolve it first", op.ID)
		}
		return nil
	case op.Base == nil:
		return guardNewSkill(op)
	default:
		return guardEditedSkill(op, checks)
	}
}

func guardNewSkill(op Op) error {
	parsed, err := jsonobject.Parse(op.Entity)
	if err != nil {
		return err
	}
	for _, name := range []string{"quality", "score", "assessment", "quarantine"} {
		if _, ok := parsed.Get(name); ok {
			return denied("new skills cannot set %s; registry automation assesses them after merge", name)
		}
	}
	var source struct {
		Kind      string `json:"kind"`
		URI       string `json:"uri"`
		Ref       string `json:"ref"`
		SkillName string `json:"skillName"`
	}
	raw, _ := parsed.Get("source")
	if err = json.Unmarshal(raw, &source); err != nil {
		return denied("skill source is invalid")
	}
	if source.Kind != "git" || !githubRepository.MatchString(strings.TrimSuffix(source.URI, ".git")) {
		return denied("registry automation only tracks https://github.com/OWNER/REPO sources")
	}
	if !fullCommit.MatchString(source.Ref) {
		return denied("source.ref must be a full 40-character commit so automation can track it")
	}
	// The registry's own tests require every pinned skill to have a location.
	if op.Location == nil || op.Location.Commit != source.Ref || !validSkillPath(op.Location.Path) {
		return denied("new skills need the SKILL.md location at their pinned commit; use Add skill")
	}
	return nil
}

func validSkillPath(path string) bool {
	return safeSkillPath.MatchString(path) && (path == "SKILL.md" || strings.HasSuffix(path, "/SKILL.md")) &&
		!strings.Contains("/"+path+"/", "/../") && !strings.Contains("/"+path+"/", "/./")
}

func guardEditedSkill(op Op, checks bool) error {
	edited, err := jsonobject.Parse(op.Entity)
	if err != nil {
		return err
	}
	base, err := jsonobject.Parse(op.Base)
	if err != nil {
		return err
	}
	changed := func(name string) bool {
		a, hasA := edited.Get(name)
		b, hasB := base.Get(name)
		return hasA != hasB || hasA && !sameJSON(a, b)
	}
	for _, name := range []string{"source", "quarantine"} {
		if changed(name) {
			return denied("%s is owned by the registry's pin workflow", name)
		}
	}
	if changed("repositoryId") && !checks {
		return denied("changing repositoryId rewrites repository scores; restart the studio with --registry-checks")
	}
	if !changed("quality") && !changed("score") && !changed("assessment") {
		return nil
	}
	if !checks {
		return denied("quality, score and assessment are projected by automation; manual assessments need --registry-checks")
	}
	var assessment struct {
		Method string `json:"method"`
	}
	raw, ok := edited.Get("assessment")
	if !ok || json.Unmarshal(raw, &assessment) != nil || assessment.Method == "" || assessment.Method == "automated" {
		return denied("only a non-automated assessment may override quality or score")
	}
	return nil
}
