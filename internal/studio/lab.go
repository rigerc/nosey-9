package studio

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/detection"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

const stateDetected = "detected"

// Assessments is the read-only automation evidence shown beside curated data.
type Assessments struct {
	Status    map[string]string `json:"status"`
	Located   map[string]string `json:"located"`
	OpenMiss  map[string]string `json:"openMiss"`
	Available bool              `json:"available"`
}

func readAssessments(root string) Assessments {
	result := Assessments{Status: map[string]string{}, Located: map[string]string{}, OpenMiss: map[string]string{}}
	if data, err := storage.Read(root, "assessments/quality.json", maxDocumentBytes); err == nil {
		var doc struct {
			Entries []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"entries"`
		}
		if json.Unmarshal(data, &doc) == nil {
			result.Available = true
			for i := range doc.Entries {
				result.Status[doc.Entries[i].ID] = doc.Entries[i].Status
			}
		}
	}
	if data, err := storage.Read(root, "assessments/locations.json", maxDocumentBytes); err == nil {
		var doc struct {
			Locations map[string]struct {
				Commit string `json:"commit"`
			} `json:"locations"`
		}
		if json.Unmarshal(data, &doc) == nil {
			for id, location := range doc.Locations {
				result.Located[id] = location.Commit
			}
		}
	}
	if data, err := storage.Read(root, "assessments/pin-probes.json", maxDocumentBytes); err == nil {
		var doc struct {
			Probes map[string]struct {
				Reason string `json:"reason"`
			} `json:"probes"`
		}
		if json.Unmarshal(data, &doc) == nil {
			for id, probe := range doc.Probes {
				result.OpenMiss[id] = probe.Reason
			}
		}
	}
	return result
}

func readProbes(root string) map[string]bool {
	open := map[string]bool{}
	for id := range readAssessments(root).OpenMiss {
		open[id] = true
	}
	return open
}

type previewResult struct {
	Skills      []domain.SkillView                 `json:"skills"`
	Unevaluated []domain.UnevaluatedRecommendation `json:"unevaluated"`
	Blocked     bool                               `json:"blocked"`
}

// handlePreview runs the real recommender over the draft for chosen technologies.
func (s *Server) handlePreview(r *http.Request) (response, error) {
	var input struct {
		Detected []string `json:"detected"`
		Unknown  []string `json:"unknown"`
	}
	if err := decodeInto(r, &input); err != nil {
		return response{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	effective, diagnostics, cleanup, err := s.stagedCatalog(r.Context(), s.draft.Ops)
	defer cleanup()
	if err != nil {
		return response{}, err
	}
	if hasErrors(diagnostics) || effective.Technologies == nil {
		return ok(previewResult{Skills: []domain.SkillView{}, Unevaluated: []domain.UnevaluatedRecommendation{}, Blocked: true})
	}
	states := make(map[string]string, len(effective.Technologies))
	for id := range effective.Technologies {
		states[id] = "absent"
	}
	for _, id := range input.Unknown {
		states[id] = "unknown"
	}
	for _, id := range input.Detected {
		states[id] = stateDetected
	}
	result := catalog.Recommend(effective, detection.Result{States: states})
	return ok(previewResult{Skills: result.Skills, Unevaluated: result.Unevaluated})
}

type sampleFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ruleTestResult struct {
	State       string               `json:"state"`
	Evidence    []domain.Evidence    `json:"evidence"`
	Gaps        []domain.CoverageGap `json:"gaps"`
	Diagnostics []domain.Diagnostic  `json:"diagnostics"`
}

// handleRuleTest evaluates one technology's rules with the detection engine,
// against a folder or a throwaway tree built from sample files.
func (s *Server) handleRuleTest(r *http.Request) (response, error) {
	var input struct {
		Technology json.RawMessage `json:"technology"`
		Folder     string          `json:"folder"`
		Files      []sampleFile    `json:"files"`
	}
	if err := decodeInto(r, &input); err != nil {
		return response{}, err
	}
	var technology domain.Technology
	if err := json.Unmarshal(input.Technology, &technology); err != nil || technology.ID == "" {
		return response{}, badRequest("technology is invalid")
	}
	root, cleanup, err := testRoot(input.Folder, input.Files)
	defer cleanup()
	if err != nil {
		return response{}, err
	}
	single := domain.Catalog{Technologies: map[string]domain.Technology{technology.ID: technology}}
	result, err := detection.Scan(r.Context(), root, single, settings.Defaults())
	if err != nil {
		return response{}, badRequest("scan failed: %s", storage.Safe(err.Error()))
	}
	out := ruleTestResult{State: result.States[technology.ID], Evidence: []domain.Evidence{}, Gaps: result.Coverage.Gaps, Diagnostics: result.Diagnostics}
	for i := range result.Detections {
		out.Evidence = append(out.Evidence, result.Detections[i].Evidence...)
	}
	return ok(out)
}

func testRoot(folder string, files []sampleFile) (string, func(), error) {
	cleanup := func() {}
	if folder != "" {
		info, err := os.Stat(folder)
		if err != nil || !info.IsDir() || !filepath.IsAbs(folder) {
			return "", cleanup, badRequest("folder must be an absolute path to a directory")
		}
		return folder, cleanup, nil
	}
	if len(files) == 0 {
		return "", cleanup, badRequest("provide a folder or at least one sample file")
	}
	dir, err := os.MkdirTemp("", "nosey-studio-rule-")
	if err != nil {
		return "", cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	for _, sample := range files {
		name := strings.TrimSpace(sample.Path)
		if name == "" || !filepath.IsLocal(name) {
			return "", cleanup, badRequest("sample path %q must be relative", sample.Path)
		}
		if err = storage.Write(filepath.Join(dir, name), []byte(sample.Content)); err != nil {
			return "", cleanup, err
		}
	}
	return dir, cleanup, nil
}
