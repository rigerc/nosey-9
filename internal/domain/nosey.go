package domain

import "encoding/json"

// Diagnostic is a sanitized, stable machine-readable problem.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Subject  string `json:"subject,omitempty"`
}

// Source identifies a catalog skill source separately from its registry.
type Source struct {
	Kind      string `json:"kind"`
	URI       string `json:"uri,omitempty"`
	Path      string `json:"path,omitempty"`
	Ref       string `json:"ref,omitempty"`
	SkillName string `json:"skillName,omitempty"`
}

// RegistrySource uses the settings contract URL/path fields, distinct from skill URIs.
type RegistrySource struct {
	Kind string `json:"kind"`
	URI  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

// RegistryRef selects an enabled data source and merge/refresh policy.
type RegistryRef struct {
	ID       string         `json:"id"`
	Source   RegistrySource `json:"source"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	TTLHours int            `json:"ttlHours,omitempty"`
}

// Rule describes one data-only detection predicate.
type Rule struct {
	Contains   string          `json:"contains,omitempty"`
	RegexFlags string          `json:"regexFlags,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Glob       string          `json:"glob,omitempty"`
	PathRegex  string          `json:"pathRegex,omitempty"`
	PathScope  string          `json:"pathScope,omitempty"`
	Ecosystem  string          `json:"ecosystem,omitempty"`
	Name       string          `json:"name,omitempty"`
	NameRegex  string          `json:"nameRegex,omitempty"`
	Pattern    string          `json:"pattern,omitempty"`
	Regex      string          `json:"regex,omitempty"`
	Extension  string          `json:"extension,omitempty"`
	Key        string          `json:"key,omitempty"`
	KeyRegex   string          `json:"keyRegex,omitempty"`
	Detector   string          `json:"detector,omitempty"`
	Disabled   bool            `json:"disabled,omitempty"`
}

// Technology is an exact canonical identity with optional detection rules.
type Technology struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Rules        []Rule `json:"rules"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
}

// Assessment records authored editorial evidence, not runtime confidence.
type Assessment struct {
	Method      string `json:"method"`
	AssessedAt  string `json:"assessedAt"`
	Assessor    string `json:"assessor,omitempty"`
	EvidenceURL string `json:"evidenceUrl,omitempty"`
}

// Skill contains effective source and relationship metadata.
type Skill struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Description   string      `json:"description,omitempty"`
	Tags          []string    `json:"tags,omitempty"`
	Technologies  []string    `json:"technologies,omitempty"`
	Source        Source      `json:"source"`
	RepositoryID  string      `json:"repositoryId,omitempty"`
	Quality       *string     `json:"quality,omitempty"`
	Score         *float64    `json:"score,omitempty"`
	Assessment    *Assessment `json:"assessment,omitempty"`
	Requires      []string    `json:"requires,omitempty"`
	ConflictsWith []string    `json:"conflictsWith,omitempty"`
	Disabled      bool        `json:"disabled,omitempty"`
	RegistryID    string      `json:"-"`
}

// Condition contains three-state technology clauses.
type Condition struct {
	All  []string `json:"allTechnologies,omitempty"`
	Any  []string `json:"anyTechnologies,omitempty"`
	None []string `json:"noneTechnologies,omitempty"`
}

// Recommendation is one conditional skill recommendation.
type Recommendation struct {
	SkillID       string     `json:"skillId"`
	Priority      string     `json:"priority,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	When          *Condition `json:"when,omitempty"`
	Requires      []string   `json:"requires,omitempty"`
	ConflictsWith []string   `json:"conflictsWith,omitempty"`
	Disabled      bool       `json:"disabled,omitempty"`
}

// Mapping anchors recommendations to a canonical technology.
type Mapping struct {
	ID              string           `json:"id"`
	TechnologyID    string           `json:"technologyId"`
	Recommendations []Recommendation `json:"recommendations"`
	Disabled        bool             `json:"disabled,omitempty"`
}

// Repository identifies an actual skill repository without rating aggregation.
type Repository struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	URI      string `json:"uri,omitempty"`
	Path     string `json:"path,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// RepositoryScore stores an authored manifest average and optional sample count.
type RepositoryScore struct {
	Average float64 `json:"averageScore"`
	Count   *int    `json:"scoreCount,omitempty"`
}

// Template points to one data-only external registry scaffold.
type Template struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Descriptor  string `json:"descriptor"`
	Description string `json:"description,omitempty"`
}

// Manifest declares versioned source documents and template descriptors.
type Manifest struct {
	Version     int                        `json:"schemaVersion"`
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	Files       map[string]string          `json:"files"`
	Templates   []Template                 `json:"templates,omitempty"`
	Scores      map[string]RepositoryScore `json:"repositoryScores,omitempty"`
}

// Catalog holds an immutable effective generation with provenance and diagnostics.
type Catalog struct {
	Raw                 map[string]map[string]json.RawMessage
	History             map[string]map[string][]string
	Sources             map[string]RegistrySource
	ProjectSources      map[string]bool
	InstallStatus       map[string]string
	Technologies        map[string]Technology
	Skills              map[string]Skill
	Mappings            map[string]Mapping
	Repositories        map[string]Repository
	Scores              map[string]RepositoryScore
	Provenance          map[string]map[string]string
	Roots               map[string]string
	Revisions           map[string]*string
	InvalidTechnologies map[string]string
	Diagnostics         []Diagnostic
}

// Evidence is sanitized positive rule evidence relative to the exact workspace.
type Evidence struct {
	RuleID      string `json:"ruleId"`
	Kind        string `json:"kind"`
	ProjectPath string `json:"projectPath"`
	Path        string `json:"path,omitempty"`
	Identifier  string `json:"matchedIdentifier,omitempty"`
}

// Detection groups positive evidence under a canonical technology ID.
type Detection struct {
	ID       string     `json:"id"`
	Evidence []Evidence `json:"evidence"`
}

// CoverageGap explains one unevaluated rule/project scope.
type CoverageGap struct {
	TechnologyID string `json:"technologyId"`
	RuleID       string `json:"ruleId"`
	Kind         string `json:"kind"`
	Reason       string `json:"reason"`
	Provider     string `json:"provider,omitempty"`
	ProjectPath  string `json:"projectPath"`
}

// Coverage reconciles required evaluation and independent technology uncertainty.
type Coverage struct {
	Required      int           `json:"requiredRuleCount"`
	Evaluated     int           `json:"evaluatedRequiredRuleCount"`
	Unevaluated   int           `json:"unevaluatedRequiredRuleCount"`
	Excluded      int           `json:"excludedRuleCount"`
	NotApplicable int           `json:"notApplicableRuleCount"`
	UnknownCount  int           `json:"unknownTechnologyCount"`
	Unknown       []string      `json:"unknownTechnologyIds"`
	Gaps          []CoverageGap `json:"gaps"`
}

// Capabilities reports supported rule/provider versions and optional modes.
type Capabilities struct {
	SupportedKinds       []string `json:"supportedRuleKinds"`
	Ecosystems           []string `json:"supportedEcosystems"`
	UnsupportedKinds     []string `json:"unsupportedRuleKinds"`
	UnavailableDetectors []string `json:"unavailableDetectors"`
	MCP                  bool     `json:"mcpPackageEnabled"`
	UnavailableRules     int      `json:"unavailableRuleCount"`
	Versions             []int    `json:"technologyDocumentVersions"`
}

// SkillView is the closed recommendation output DTO.
type SkillView struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Status            string      `json:"status"`
	State             string      `json:"recommendationState"`
	Priority          string      `json:"priority"`
	Technologies      []string    `json:"technologyIds"`
	Reasons           []string    `json:"reasons"`
	RepositoryID      string      `json:"repositoryId"`
	RegistryID        string      `json:"sourceRegistryId"`
	Selected          bool        `json:"selected"`
	Quality           *string     `json:"quality"`
	Score             *float64    `json:"score"`
	RepositoryAverage *float64    `json:"repositoryAverage"`
	Assessment        *Assessment `json:"assessment"`
}

// UnevaluatedRecommendation records mappings blocked by unknown technology facts.
type UnevaluatedRecommendation struct {
	SkillID   string   `json:"skillId"`
	MappingID string   `json:"mappingId"`
	State     string   `json:"recommendationState"`
	Unknown   []string `json:"unknownTechnologies"`
	Reason    string   `json:"reason"`
}

// Filters contains normalized presentation-only facets.
type Filters struct {
	Query                      string   `json:"query"`
	QueryTarget                string   `json:"queryTarget"`
	Technologies               []string `json:"technologies"`
	RecommendationTechnologies []string `json:"recommendationTechnologies"`
	RuleKinds                  []string `json:"ruleKinds"`
	Registries                 []string `json:"registryIds"`
	Repositories               []string `json:"repositoryIds"`
	Priorities                 []string `json:"priorities"`
	Statuses                   []string `json:"statuses"`
	Qualities                  []string `json:"qualities"`
	ScoreState                 string   `json:"scoreState"`
	MinScore                   *float64 `json:"minScore"`
	MinRepositoryAverage       *float64 `json:"minRepositoryAverage"`
}

// Sort contains independent technology/recommendation comparators.
type Sort struct {
	Technologies string `json:"technologies"`
	Skills       string `json:"skills"`
}

// ScanResult is the versioned scan output envelope.
type ScanResult struct {
	Version             int                         `json:"outputVersion"`
	Diagnostics         []Diagnostic                `json:"diagnostics"`
	Complete            bool                        `json:"complete"`
	Technologies        []Detection                 `json:"detectedTechnologies"`
	Skills              []SkillView                 `json:"recommendedSkills"`
	TechnologyTotal     int                         `json:"detectedTechnologiesTotal"`
	VisibleTechnologies int                         `json:"visibleTechnologiesTotal"`
	SkillTotal          int                         `json:"recommendedSkillsTotal"`
	VisibleSkills       int                         `json:"visibleRecommendedSkillsTotal"`
	Filters             Filters                     `json:"filters"`
	Sort                Sort                        `json:"sort"`
	Unevaluated         []UnevaluatedRecommendation `json:"unevaluatedRecommendations"`
	Coverage            Coverage                    `json:"coverage"`
	Capabilities        Capabilities                `json:"capabilities"`
}

// DefaultFilters returns all required wire defaults with non-nil arrays.
func DefaultFilters() Filters {
	return Filters{QueryTarget: "both", Technologies: []string{}, RecommendationTechnologies: []string{}, RuleKinds: []string{}, Registries: []string{}, Repositories: []string{}, Priorities: []string{}, Statuses: []string{}, Qualities: []string{}, ScoreState: "any"}
}

// RawEntity preserves field presence for overlay merging.
type RawEntity map[string]json.RawMessage
