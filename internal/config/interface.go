// Package config resolves application-owned settings with koanf.
// Manager is the optional TOML persistence adapter for the UI example.
package config

import "github.com/go-cli-template/internal/domain"

// Manager describes configuration persistence behavior.
type Manager interface {
	Load() (domain.Config, error)
	Save(config domain.Config) error
	Exists() (bool, error)
}
