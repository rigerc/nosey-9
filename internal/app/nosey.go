package app

import (
	"context"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/detection"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/settings"
)

// Session holds one consistent settings/catalog generation for a command or UI refresh.
type Session struct {
	Store     settings.Store
	Resolved  settings.Resolved
	Catalog   domain.Catalog
	Registry  registry.Service
	Installer installation.Service
}

// Open resolves one immutable catalog generation from the exact workspace.
func Open(ctx context.Context, cwd, config string, refresh bool) (Session, error) {
	store, err := settings.New(cwd, config)
	if err != nil {
		return Session{}, err
	}
	resolved, err := store.Resolve()
	if err != nil {
		return Session{}, err
	}
	s := configure(store, resolved)
	c, err := s.Registry.Load(ctx, resolved, refresh)
	if err != nil {
		return Session{}, err
	}
	s.Catalog = c
	return s, nil
}

// Configure opens settings and installation services without requiring a registry fetch.
func Configure(cwd, config string) (Session, error) {
	store, err := settings.New(cwd, config)
	if err != nil {
		return Session{}, err
	}
	resolved, err := store.Resolve()
	if err != nil {
		return Session{}, err
	}
	return configure(store, resolved), nil
}
func configure(store settings.Store, resolved settings.Resolved) Session {
	return Session{Store: store, Resolved: resolved, Registry: registry.Service{Store: store, Runner: process.Runner{}}, Installer: installation.Service{Store: store, Settings: resolved.Settings, Runner: process.Runner{}}}
}

// Scan detects and recommends before applying presentation-only projections.
func (s Session) Scan(ctx context.Context, filters domain.Filters, sort domain.Sort, mcp bool) (domain.ScanResult, catalog.Recommendations, error) {
	if err := catalog.ValidateFilters(s.Catalog, filters); err != nil {
		return domain.ScanResult{}, catalog.Recommendations{}, err
	}
	if err := catalog.ValidateSort(sort.Technologies, true, false); err != nil {
		return domain.ScanResult{}, catalog.Recommendations{}, err
	}
	if err := catalog.ValidateSort(sort.Skills, false, false); err != nil {
		return domain.ScanResult{}, catalog.Recommendations{}, err
	}
	cfg := s.Resolved.Settings
	if mcp {
		cfg.Detection.MCP = true
	}
	detected, err := detection.Scan(ctx, s.Store.Workspace, s.Catalog, cfg)
	if err != nil {
		return domain.ScanResult{}, catalog.Recommendations{}, err
	}
	recommendations := catalog.Recommend(s.Catalog, detected)
	result := domain.ScanResult{Version: 2, Diagnostics: append(s.Catalog.Diagnostics, detected.Diagnostics...), Complete: detected.Coverage.Unevaluated == 0, Technologies: detected.Detections, Skills: recommendations.Skills, TechnologyTotal: len(detected.Detections), SkillTotal: len(recommendations.Skills), Unevaluated: recommendations.Unevaluated, Coverage: detected.Coverage, Capabilities: detected.Capabilities}
	return catalog.Project(s.Catalog, result, filters, sort), recommendations, nil
}

// Inventory refreshes installed status without re-running detection.
func (s *Session) Inventory(ctx context.Context) installation.Inventory {
	inventory := s.Installer.Inventory(ctx, s.Catalog)
	s.ApplyInventory(inventory)
	return inventory
}

// ApplyInventory updates installed-state projections without re-running detection.
func (s *Session) ApplyInventory(inventory installation.Inventory) {
	s.Catalog.InstallStatus = map[string]string{}
	if inventory.Complete {
		for id, skill := range s.Catalog.Skills {
			status := "missing"
			for _, record := range inventory.Skills {
				if record.Name != skill.Source.SkillName {
					continue
				}
				if record.SkillID != nil && *record.SkillID == id {
					status = record.Status
					break
				}
				status = "unknown"
			}
			s.Catalog.InstallStatus[id] = status
		}
	}
}
