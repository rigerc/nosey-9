// Package detection evaluates bounded workspace observations with explicit unknown states.
package detection

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
)

// Result contains technology states, positive evidence and execution coverage.
type Result struct {
	Detections   []domain.Detection
	States       map[string]string
	Coverage     domain.Coverage
	Capabilities domain.Capabilities
	Diagnostics  []domain.Diagnostic
}

type file struct {
	directory bool
	unsafe    bool
	path      string
	data      []byte
	err       error
}

type observation struct{ ecosystem, name, path string }

type engine struct {
	projects       []string
	scope          string
	ctx            context.Context
	root           string
	settings       settings.Settings
	files          []file
	observations   []observation
	patterns       map[string]*regexp2.Regexp
	truncated      bool
	providerErrors map[string]bool
}

var kinds = []string{"file", "dependency", "content", "extension", "dotenv", "detector", "mcp_package"}

var ecosystems = []string{"npm", "python", "cargo", "go", "ruby", "composer", "docker", "terraform", "terraform.resource", "githubAction", "deno", "hex"}

// Scan evaluates effective rules inside the exact workspace root.
func Scan(ctx context.Context, root string, c domain.Catalog, cfg settings.Settings) (Result, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	e := engine{ctx: ctx, root: root, settings: cfg, patterns: map[string]*regexp2.Regexp{}, providerErrors: map[string]bool{}}
	if err := e.walk(); err != nil {
		if ctx.Err() != nil && parent.Err() == nil {
			e.truncated = true
			if len(e.projects) == 0 {
				e.projects = []string{"."}
			}
		} else {
			return Result{}, err
		}
	}
	e.observe()
	slices.SortFunc(e.observations, func(a, b observation) int {
		if a.ecosystem != b.ecosystem {
			return strings.Compare(a.ecosystem, b.ecosystem)
		}
		if a.path != b.path {
			return strings.Compare(a.path, b.path)
		}
		return strings.Compare(a.name, b.name)
	})
	e.observations = slices.Compact(e.observations)
	r := Result{Detections: []domain.Detection{}, States: map[string]string{}, Diagnostics: []domain.Diagnostic{}, Coverage: domain.Coverage{Unknown: []string{}, Gaps: []domain.CoverageGap{}}, Capabilities: domain.Capabilities{SupportedKinds: slices.Clone(kinds), Ecosystems: slices.Clone(ecosystems), UnsupportedKinds: []string{}, UnavailableDetectors: []string{}, MCP: cfg.Detection.MCP, Versions: []int{1, 2}}}
	ids := []string{}
	for id := range c.Technologies {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		t := c.Technologies[id]
		state := "absent"
		evidence := []domain.Evidence{}
		active := 0
		for _, rule := range t.Rules {
			for _, project := range e.projects {
				e.scope = project
				if rule.Disabled {
					continue
				}
				active++
				if (rule.Kind == "mcp_package" && !cfg.Detection.MCP) || (rule.Kind == "detector" && !e.adapterEnabled(rule.Detector)) {
					r.Coverage.Excluded++
					r.Coverage.Gaps = append(r.Coverage.Gaps, domain.CoverageGap{TechnologyID: id, RuleID: rule.ID, Kind: rule.Kind, Reason: "excluded-by-mode", ProjectPath: project})
					state = "unknown"
					continue
				}
				r.Coverage.Required++
				matched, err := e.evaluate(rule)
				if err != nil || e.truncated {
					r.Coverage.Unevaluated++
					reason := "execution-error"
					if e.truncated {
						reason = "budget-exceeded"
					}
					r.Coverage.Gaps = append(r.Coverage.Gaps, domain.CoverageGap{TechnologyID: id, RuleID: rule.ID, Kind: rule.Kind, Reason: reason, Provider: rule.Ecosystem, ProjectPath: project})
					state = "unknown"
				} else {
					r.Coverage.Evaluated++
				}
				evidence = append(evidence, matched...)
			}
		}
		if len(evidence) > 0 {
			state = "detected"
			r.Detections = append(r.Detections, domain.Detection{ID: id, Evidence: evidence})
		} else if active == 0 {
			state = "unknown"
		}
		r.States[id] = state
		if state == "unknown" {
			r.Coverage.Unknown = append(r.Coverage.Unknown, id)
		}
	}
	for id := range c.InvalidTechnologies {
		r.States[id] = "unknown"
		r.Coverage.Unknown = append(r.Coverage.Unknown, id)
		r.Coverage.Required++
		r.Coverage.Unevaluated++
		r.Coverage.Gaps = append(r.Coverage.Gaps, domain.CoverageGap{TechnologyID: id, RuleID: "invalid-effective-definition", Kind: "detector", Reason: "other", ProjectPath: "."})
	}
	slices.Sort(r.Coverage.Unknown)
	r.Coverage.UnknownCount = len(r.Coverage.Unknown)
	r.Capabilities.UnavailableRules = r.Coverage.Unevaluated
	return r, nil
}
