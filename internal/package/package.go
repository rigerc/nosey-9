// Package pkg exposes project metadata embedded from package.toml.
package pkg

import (
	_ "embed"
	"strings"
)

//go:embed package.toml
var packageToml string

// Parse extracts a value from package.toml
func Parse(key string) string {
	for _, line := range strings.Split(packageToml, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+" = ") {
			value := strings.TrimPrefix(line, key+" = ")
			value = strings.Trim(value, `"`)
			return value
		}
	}
	return ""
}

// Name returns the project name from package.toml
func Name() string {
	name := Parse("name")
	if name == "" {
		// package.toml is embedded at build time, so a missing name is a build
		// defect that cannot be recovered from at runtime.
		panic("package.toml: 'name' field is required but not found")
	}
	return name
}

// Version returns the version from package.toml
func Version() string {
	return Parse("version")
}

// Short returns the short description from package.toml
func Short() string {
	return Parse("short")
}

// Description returns the description from package.toml
func Description() string {
	return Parse("description")
}
