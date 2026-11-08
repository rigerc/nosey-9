package detection

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/storage"
)

func (e *engine) adapterEnabled(id string) bool {
	for _, a := range e.settings.Detection.Adapters {
		if a.ID == id && a.Enabled {
			return true
		}
	}
	return false
}

func (e *engine) runAdapter(rule domain.Rule) ([]domain.Evidence, error) {
	for _, a := range e.settings.Detection.Adapters {
		if a.ID != rule.Detector || !a.Enabled {
			continue
		}
		if !filepath.IsAbs(a.Path) {
			return nil, fmt.Errorf("adapter path must be absolute")
		}
		data, err := os.ReadFile(a.Path)
		if err != nil {
			return nil, err
		}
		if storage.Hash(data) != a.Hash {
			return nil, fmt.Errorf("adapter hash mismatch")
		}
		request, err := json.Marshal(struct {
			Version        int         `json:"protocolVersion"`
			AdapterID      string      `json:"adapterId"`
			AdapterVersion string      `json:"adapterVersion"`
			Workspace      string      `json:"workspace"`
			ProjectPath    string      `json:"projectPath"`
			Rule           domain.Rule `json:"rule"`
		}{1, a.ID, a.Version, e.root, e.scope, rule})
		if err != nil {
			return nil, err
		}
		out, err := (process.Runner{Timeout: 5 * time.Second, Limit: 1 << 20}).RunInput(e.ctx, e.root, []string{"NOSEY_ADAPTER_VERSION=" + a.Version}, request, a.Path)
		if err != nil {
			return nil, err
		}
		var items []domain.Evidence
		if err = json.Unmarshal(out.Stdout, &items); err != nil {
			return nil, err
		}
		for i := range items {
			items[i].RuleID = rule.ID
			items[i].Kind = rule.Kind
			items[i].Identifier = "" // Custom adapters may not introduce arbitrary secret-bearing identifiers.
			if !filepath.IsLocal(items[i].Path) {
				return nil, fmt.Errorf("adapter evidence escapes workspace")
			}
			if _, err := storage.Read(e.root, items[i].Path, e.settings.Detection.MaxBytes); err != nil {
				return nil, fmt.Errorf("adapter evidence must identify an accessible confined file")
			}
			if e.projectFor(items[i].Path) != e.scope {
				return nil, fmt.Errorf("adapter evidence escapes project scope")
			}
			items[i].Path = storage.Safe(items[i].Path)
			items[i].ProjectPath = e.scope
		}
		return items, nil
	}
	return nil, fmt.Errorf("required adapter unavailable")
}
