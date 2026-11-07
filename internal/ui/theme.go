// Package ui provides inline Bubble Tea UI components.
package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/go-cli-template/internal/domain"
)

// Theme holds configurable colors for UI output.
type Theme struct {
	Headings             color.Color
	Primary              color.Color
	Secondary            color.Color
	Text                 color.Color
	TextHighlight        color.Color
	DescriptionHighlight color.Color
	Tags                 color.Color
	Flags                color.Color
	Muted                color.Color
	Border               color.Color
}

// ThemeFromConfig builds a theme with safe fallbacks.
func ThemeFromConfig(cfg domain.Config) Theme {
	return Theme{
		Headings:             resolveColor(cfg.Headings, "15"),
		Primary:              resolveColor(cfg.Primary, "02"),
		Secondary:            resolveColor(cfg.Secondary, "06"),
		Text:                 resolveColor(cfg.Text, "07"),
		TextHighlight:        resolveColor(resolveFallback(cfg.TextHighlight, cfg.Secondary), "06"),
		DescriptionHighlight: resolveColor(resolveFallback(cfg.DescriptionHighlight, cfg.Secondary), "06"),
		Tags:                 resolveColor(resolveFallback(cfg.Tags, cfg.Accent), "13"),
		Flags:                resolveColor(cfg.Flags, "12"),
		Muted:                resolveColor(cfg.Muted, "08"),
		Border:               resolveColor(cfg.Border, "08"),
	}
}

func resolveColor(value, fallback string) color.Color {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = fallback
	}
	return lipgloss.Color(trimmed)
}

func resolveFallback(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
