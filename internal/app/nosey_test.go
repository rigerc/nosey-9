package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

func TestLocalSessionScanAndInventoryProjection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	official, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := settings.New(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Registries([]domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: official}, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err = storage.Write(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"next":"1"}}`)); err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	scan, _, err := session.Scan(context.Background(), domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
	if err != nil || !scan.Complete || len(scan.Skills) == 0 {
		t.Fatal(scan.Coverage, err)
	}
	chosen := scan.Skills[0].ID
	session.ApplyInventory(installation.Inventory{Complete: true, Skills: []installation.InventoryItem{{SkillID: &chosen, Name: session.Catalog.Skills[chosen].Source.SkillName, Status: "installed"}}})
	if session.Catalog.InstallStatus[chosen] != "installed" {
		t.Fatal("verified inventory was not applied")
	}
	session.ApplyInventory(installation.Inventory{Complete: false})
	if len(session.Catalog.InstallStatus) != 0 {
		t.Fatal("unavailable inventory retained missing certainty")
	}
	basic, err := Configure(root, "")
	if err != nil || len(basic.Catalog.Skills) != 0 {
		t.Fatal("settings-only session loaded catalog", err)
	}
	if _, err = Open(context.Background(), filepath.Join(root, "missing"), "", false); err == nil {
		t.Fatal("missing root accepted")
	}
	if err = os.WriteFile(store.UserPath, []byte(`{"version":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Configure(root, ""); err == nil {
		t.Fatal("legacy settings accepted")
	}
}
