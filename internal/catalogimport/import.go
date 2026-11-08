package catalogimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	idPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$`)
	upstreamIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	namePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	revisionPattern    = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
	categorySeparators = regexp.MustCompile(`[^a-z0-9]+`)
)

// Compile validates and converts a frozen verified catalog without side effects.
func Compile(in Inputs) (*Result, error) {
	if !revisionPattern.MatchString(in.Revision) {
		return nil, fmt.Errorf("invalid catalog revision %q", in.Revision)
	}
	var ledger ledgerDocument
	var catalog catalogDocument
	var author mappingDocument
	var tech technologyDocument
	var crosswalk map[string]string
	for _, input := range []struct {
		name   string
		data   []byte
		target any
	}{
		{"ledger", in.Ledger, &ledger}, {"catalog", in.Catalog, &catalog}, {"mappings", in.Mappings, &author},
		{"technologies", in.Technologies, &tech}, {"technology map", in.TechnologyMap, &crosswalk},
	} {
		if err := json.Unmarshal(input.data, input.target); err != nil {
			return nil, fmt.Errorf("%s: %w", input.name, err)
		}
	}
	if ledger.Version != 1 || catalog.Version != 1 || author.Version != 1 || tech.Version != 2 {
		return nil, fmt.Errorf("unsupported input schema versions: ledger=%d catalog=%d mappings=%d technologies=%d", ledger.Version, catalog.Version, author.Version, tech.Version)
	}
	if len(ledger.Entries) == 0 || len(author.Technologies) == 0 {
		return nil, fmt.Errorf("empty skill ledger or technology mappings")
	}
	base, err := technologyIndex(tech.Technologies)
	if err != nil {
		return nil, err
	}
	authorIndex := make(map[string]authorTechnology, len(author.Technologies))
	for _, t := range author.Technologies {
		if _, exists := authorIndex[t.ID]; exists {
			return nil, fmt.Errorf("duplicate author technology %q", t.ID)
		}
		target, ok := crosswalk[t.ID]
		if !ok || target == "" {
			return nil, fmt.Errorf("technology %q has no explicit crosswalk", t.ID)
		}
		if _, exists := base[target]; !exists {
			// New identities retain exact source IDs; aliases must target existing definitions.
			if target != t.ID || !idPattern.MatchString(target) || t.Name == "" {
				return nil, fmt.Errorf("invalid new technology %q -> %q", t.ID, target)
			}
			category := strings.Trim(categorySeparators.ReplaceAllString(strings.ToLower(t.Category), "-"), "-")
			if category == "" {
				category = "imported"
			}
			raw, marshalErr := json.Marshal(struct {
				ID           string            `json:"id"`
				Name         string            `json:"name"`
				Category     string            `json:"category"`
				MetadataOnly bool              `json:"metadataOnly"`
				Rules        []json.RawMessage `json:"rules"`
			}{target, t.Name, category, true, []json.RawMessage{}})
			if marshalErr != nil {
				return nil, marshalErr
			}
			base[target] = raw
		}
		authorIndex[t.ID] = t
	}
	for id := range crosswalk {
		if _, ok := authorIndex[id]; !ok {
			return nil, fmt.Errorf("crosswalk contains unknown author technology %q", id)
		}
	}
	metadata := make(map[string]catalogSkill, len(catalog.Skills))
	for _, s := range catalog.Skills {
		if _, ok := metadata[s.ID]; ok {
			return nil, fmt.Errorf("duplicate catalog skill %q", s.ID)
		}
		metadata[s.ID] = s
	}
	skills := make(map[string]*skill, len(ledger.Entries))
	skillIDs := make(map[string]string, len(ledger.Entries))
	provenance := make(map[string]skillProvenance, len(ledger.Entries))
	repositories := make(map[string]repository)
	identities := make(map[string]string)
	for _, entry := range ledger.Entries {
		if _, ok := skills[entry.ID]; ok {
			return nil, fmt.Errorf("duplicate ledger skill %q", entry.ID)
		}
		meta, ok := metadata[entry.ID]
		if !ok {
			return nil, fmt.Errorf("ledger skill %q missing catalog metadata", entry.ID)
		}
		if !upstreamIDPattern.MatchString(entry.ID) || strings.Contains(entry.ID, "//") {
			return nil, fmt.Errorf("invalid upstream skill ID %q", entry.ID)
		}
		id := strings.ReplaceAll(entry.ID, "/", ":")
		if !idPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid converted skill ID %q", id)
		}
		name := entry.SkillName
		if name == "" {
			name = meta.Name
		}
		if !namePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid installation name %q for %s", name, entry.ID)
		}
		repo, sourcePath, sourceErr := parseSource(entry)
		if sourceErr != nil {
			return nil, sourceErr
		}
		repoID := strings.ReplaceAll(strings.ToLower(repo), "/", ":")
		uri := "https://github.com/" + repo
		if existing, ok := repositories[repoID]; ok {
			uri = existing.URI
		} else {
			repositories[repoID] = repository{repoID, repo, "git", uri}
		}
		identity := strings.ToLower(repo) + "#" + entry.Commit + "/" + name
		if previous, exists := identities[identity]; exists {
			return nil, fmt.Errorf("ambiguous installation identity: %s and %s", previous, entry.ID)
		}
		identities[identity] = entry.ID
		skills[entry.ID] = &skill{id, name, []string{}, source{"git", uri, entry.Commit, name}, repoID}
		skillIDs[entry.ID] = id
		provenance[entry.ID] = skillProvenance{id, entry.URL, sourcePath, entry.Commit, name}
	}
	for id := range metadata {
		if _, ok := skills[id]; !ok {
			return nil, fmt.Errorf("catalog skill %q missing verified ledger record", id)
		}
	}
	mappings := make(map[string]mapping)
	addMapping := func(id, anchor, reason string, required, refs []string) error {
		if !idPattern.MatchString(id) {
			return fmt.Errorf("invalid mapping ID %q", id)
		}
		if _, ok := mappings[id]; ok {
			return fmt.Errorf("duplicate mapping %q", id)
		}
		required = unique(required)
		items := []recommendation{}
		seen := make(map[string]bool)
		for _, ref := range refs {
			s, ok := skills[ref]
			if !ok {
				return fmt.Errorf("mapping %s references unknown skill %q", id, ref)
			}
			if seen[s.ID] {
				return fmt.Errorf("mapping %s repeats skill %q", id, ref)
			}
			seen[s.ID] = true
			r := recommendation{SkillID: s.ID, Priority: "optional", Reason: reason}
			if len(required) > 0 {
				r.When = &condition{required}
			}
			items = append(items, r)
			s.Technologies = append(s.Technologies, anchor)
			s.Technologies = append(s.Technologies, required...)
		}
		slices.SortFunc(items, func(a, b recommendation) int { return strings.Compare(a.SkillID, b.SkillID) })
		mappings[id] = mapping{id, anchor, items}
		return nil
	}
	for _, t := range author.Technologies {
		if len(t.SkillIDs) == 0 {
			continue
		}
		if err = addMapping("technology:"+strings.NewReplacer("@", "scope:", "/", ":").Replace(t.ID), crosswalk[t.ID], "Catalog recommendation for "+t.Name+".", nil, t.SkillIDs); err != nil {
			return nil, err
		}
	}
	combinationIDs := make(map[string]bool)
	for _, c := range author.Combinations {
		if combinationIDs[c.ID] {
			return nil, fmt.Errorf("duplicate combination %q", c.ID)
		}
		combinationIDs[c.ID] = true
		required := []string{}
		for _, id := range c.Requires {
			target, ok := crosswalk[id]
			if !ok {
				return nil, fmt.Errorf("combination %s references unknown technology %q", c.ID, id)
			}
			required = append(required, target)
		}
		required = unique(required)
		if len(required) == 0 || len(c.SkillIDs) == 0 {
			return nil, fmt.Errorf("empty combination %q", c.ID)
		}
		if err = addMapping("combination:"+c.ID, required[0], "Catalog combination: "+c.Name+".", required[1:], c.SkillIDs); err != nil {
			return nil, err
		}
	}
	result := &Result{Files: make(map[string][]byte), Skills: len(skills), Repositories: len(repositories), Technologies: len(base), Mappings: len(mappings), Combinations: len(author.Combinations)}
	skillList := make([]skill, 0, len(skills))
	for _, s := range skills {
		s.Technologies = unique(s.Technologies)
		skillList = append(skillList, *s)
	}
	slices.SortFunc(skillList, func(a, b skill) int { return strings.Compare(a.ID, b.ID) })
	repoList := make([]repository, 0, len(repositories))
	for _, r := range repositories {
		repoList = append(repoList, r)
	}
	slices.SortFunc(repoList, func(a, b repository) int { return strings.Compare(a.ID, b.ID) })
	mappingList := make([]mapping, 0, len(mappings))
	for _, m := range mappings {
		mappingList = append(mappingList, m)
	}
	slices.SortFunc(mappingList, func(a, b mapping) int { return strings.Compare(a.ID, b.ID) })
	techIDs := make([]string, 0, len(base))
	for id := range base {
		techIDs = append(techIDs, id)
	}
	slices.Sort(techIDs)
	techList := make([]json.RawMessage, 0, len(base))
	for _, id := range techIDs {
		techList = append(techList, base[id])
	}
	manifest := struct {
		Version   int                 `json:"schemaVersion"`
		ID        string              `json:"id"`
		Name      string              `json:"name"`
		Files     map[string]string   `json:"files"`
		Templates []map[string]string `json:"templates"`
	}{1, "nosey:official", "Official Nosey Registry", map[string]string{"technologies": "technologies.json", "skills": "skills.json", "mappings": "mappings.json", "repositories": "repositories.json"}, []map[string]string{{"id": "empty", "kind": "registry", "descriptor": "templates/registry/empty/nosey.template.json"}, {"id": "workspace", "kind": "registry", "descriptor": "templates/registry/workspace/nosey.template.json"}}}
	outputs := map[string]any{
		"nosey.registry.json": manifest,
		"skills.json": struct {
			Version int     `json:"schemaVersion"`
			Skills  []skill `json:"skills"`
		}{2, skillList},
		"mappings.json": struct {
			Version  int       `json:"schemaVersion"`
			Mappings []mapping `json:"mappings"`
		}{2, mappingList},
		"repositories.json": struct {
			Version      int          `json:"schemaVersion"`
			Repositories []repository `json:"repositories"`
		}{1, repoList},
		"technologies.json":           technologyDocument{2, techList},
		"authoring/skill-id-map.json": skillIDs, "authoring/technology-id-map.json": crosswalk, "authoring/sources.json": provenance,
		"authoring/import-manifest.json": struct {
			Version       int               `json:"schemaVersion"`
			Revision      string            `json:"sourceRevision"`
			Hashes        map[string]string `json:"sha256"`
			Skills        int               `json:"skills"`
			Technologies  int               `json:"technologies"`
			Mappings      int               `json:"mappings"`
			Combinations  int               `json:"combinations"`
			FrontendBonus string            `json:"frontendBonus"`
		}{1, in.Revision, map[string]string{"catalog-source-ledger.json": digest(in.Ledger), "catalog.json": digest(in.Catalog), "mappings.json": digest(in.Mappings), "technology-reference.json": digest(in.Technologies), "technology-id-map.json": digest(in.TechnologyMap)}, result.Skills, result.Technologies, result.Mappings, result.Combinations, "ignored"},
	}
	for name, doc := range outputs {
		data, marshalErr := json.MarshalIndent(doc, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("%s: %w", name, marshalErr)
		}
		result.Files[name] = append(data, '\n')
	}
	result.Files["authoring/technology-reference.json"] = slices.Clone(in.Technologies)
	return result, nil
}

func parseSource(e ledgerEntry) (string, string, error) {
	u, err := url.Parse(e.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("invalid verified source URL for %s", e.ID)
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" || parts[3] != e.Commit || !revisionPattern.MatchString(e.Commit) || parts[len(parts)-1] != "SKILL.md" || path.Clean(u.Path) != u.Path {
		return "", "", fmt.Errorf("source commit/path mismatch for %s", e.ID)
	}
	if !namePattern.MatchString(parts[0]) || !namePattern.MatchString(parts[1]) {
		return "", "", fmt.Errorf("invalid repository for %s", e.ID)
	}
	return strings.Join(parts[:2], "/"), strings.Join(parts[4:], "/"), nil
}

func technologyIndex(entries []json.RawMessage) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(entries))
	for _, raw := range entries {
		var t struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		if t.ID == "" {
			return nil, fmt.Errorf("technology reference contains empty ID")
		}
		if _, ok := result[t.ID]; ok {
			return nil, fmt.Errorf("duplicate reference technology %q", t.ID)
		}
		result[t.ID] = raw
	}
	return result, nil
}

func unique(values []string) []string {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
