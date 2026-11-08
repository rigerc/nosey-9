// Package catalogimport converts verified authoring catalogs into Nosey registry data.
// It performs no network access or skill installation.
package catalogimport

import "encoding/json"

// Inputs contains frozen source bytes and an explicit technology identity map.
type Inputs struct {
	Ledger, Catalog, Mappings, Technologies, TechnologyMap []byte
	Revision                                               string
}

// Result is a complete staged registry. Paths are relative to its root.
type Result struct {
	Files                                                      map[string][]byte
	Skills, Repositories, Technologies, Mappings, Combinations int
}

type ledgerDocument struct {
	Version int           `json:"schema_version"`
	Entries []ledgerEntry `json:"entries"`
}
type ledgerEntry struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Commit    string `json:"commit"`
	SkillName string `json:"source_skill_name"`
}
type catalogDocument struct {
	Version int            `json:"schema_version"`
	Skills  []catalogSkill `json:"skills"`
}
type catalogSkill struct {
	ID   string `json:"id"`
	Name string `json:"skill_name"`
}
type mappingDocument struct {
	Version      int                 `json:"schema_version"`
	Technologies []authorTechnology  `json:"technologies"`
	Combinations []authorCombination `json:"combinations"`
}
type authorTechnology struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	SkillIDs []string `json:"skill_ids"`
}
type authorCombination struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Requires []string `json:"requires"`
	SkillIDs []string `json:"skill_ids"`
}
type technologyDocument struct {
	Version      int               `json:"schemaVersion"`
	Technologies []json.RawMessage `json:"technologies"`
}
type skill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Technologies []string `json:"technologies"`
	Source       source   `json:"source"`
	RepositoryID string   `json:"repositoryId"`
}
type source struct {
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	Ref       string `json:"ref"`
	SkillName string `json:"skillName"`
}
type repository struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URI  string `json:"uri"`
}
type mapping struct {
	ID              string           `json:"id"`
	TechnologyID    string           `json:"technologyId"`
	Recommendations []recommendation `json:"recommendations"`
}
type recommendation struct {
	SkillID  string     `json:"skillId"`
	Priority string     `json:"priority"`
	Reason   string     `json:"reason"`
	When     *condition `json:"when,omitempty"`
}
type condition struct {
	All []string `json:"allTechnologies"`
}
type skillProvenance struct {
	ID         string `json:"id"`
	SourceURL  string `json:"sourceUrl"`
	SourcePath string `json:"sourcePath"`
	Commit     string `json:"commit"`
	SkillName  string `json:"skillName"`
}
