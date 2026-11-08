package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/utils"
)

// ManagerImpl loads and saves configuration files.
type ManagerImpl struct {
	cwd string
}

// NewManager returns a config manager rooted at the provided cwd.
func NewManager(cwd string) *ManagerImpl {
	return &ManagerImpl{cwd: cwd}
}

// LoadWithOverride loads config from a specific path, layered on defaults.
func (m *ManagerImpl) LoadWithOverride(path string) (domain.Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return m.Load()
	}
	return Load(domain.DefaultConfig(), Options[domain.Config]{
		Files: []FileSource{{Path: path}}, Validate: validateConfig,
	})
}

// Load reads config with precedence: defaults < global < local.
func (m *ManagerImpl) Load() (domain.Config, error) {
	return Load(domain.DefaultConfig(), Options[domain.Config]{
		Files: []FileSource{
			{Path: utils.ConfigPathGlobal(), Optional: true},
			{Path: utils.ConfigPathLocal(m.cwd), Optional: true},
		},
		Validate: validateConfig,
	})
}

func validateConfig(config domain.Config) error {
	switch config.ListSpacing {
	case "compact", "tight", "space":
		return nil
	default:
		return fmt.Errorf("invalid list_spacing %q: expected compact, tight, or space", config.ListSpacing)
	}
}

// Save persists config to the global config path.
func (m *ManagerImpl) Save(config domain.Config) error {
	path := utils.ConfigPathGlobal()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := toml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Exists reports whether a local or global config file exists.
func (m *ManagerImpl) Exists() (bool, error) {
	globalPath := utils.ConfigPathGlobal()
	if exists, err := fileExists(globalPath); err != nil {
		return false, err
	} else if exists {
		return true, nil
	}
	localPath := utils.ConfigPathLocal(m.cwd)
	return fileExists(localPath)
}

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
