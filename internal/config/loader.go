package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	fatih "github.com/fatih/structs"
	"github.com/go-viper/mapstructure/v2"
	tomlparser "github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/pelletier/go-toml/v2"
)

// FileSource is one TOML layer. Optional skips missing files and directories,
// matching the starter's global/local file selection; other read errors fail.
type FileSource struct {
	Path     string
	Optional bool
}

// EnvSource explicitly maps environment names to dotted configuration keys.
// Lookup is required; the loader never implicitly reads the process environment.
type EnvSource struct {
	Variables map[string]string
	Lookup    func(string) (string, bool)
	// ParseValues enables string-to-basic-type conversion for this source only.
	// TOML decoding remains strict, and strings such as "02" remain strings.
	ParseValues bool
}

// Options selects sources and application validation. The struct tag defaults
// to "toml". Files load in order, followed by explicitly mapped environment values.
type Options[T any] struct {
	Files    []FileSource
	Env      *EnvSource
	Tag      string
	Validate func(T) error
}

// Load resolves a fresh typed configuration without modifying defaults or
// retaining state. T must be a struct (or a non-nil pointer to a struct).
// Application services receive T, never a mutable koanf instance.
func Load[T any](defaults T, opts Options[T]) (T, error) {
	var zero T
	if !fatih.IsStruct(defaults) {
		return zero, errors.New("configuration defaults must be a non-nil struct")
	}
	tag := opts.Tag
	if tag == "" {
		tag = "toml"
	}
	k := koanf.New(".")
	if err := k.Load(structs.Provider(defaults, tag), nil); err != nil {
		return zero, fmt.Errorf("load defaults: %w", err)
	}
	schema := k.Raw()
	for _, source := range opts.Files {
		if strings.TrimSpace(source.Path) == "" {
			return zero, errors.New("config file path must not be empty")
		}
		info, err := os.Stat(source.Path)
		if errors.Is(err, os.ErrNotExist) || err == nil && info.IsDir() {
			if source.Optional {
				continue
			}
			return zero, fmt.Errorf("config file not found: %s", source.Path)
		}
		if err != nil {
			return zero, fmt.Errorf("config file %s: %w", source.Path, err)
		}
		layer := koanf.New(".")
		if err := layer.Load(file.Provider(source.Path), checkedTOML[T]{tag: tag}); err != nil {
			return zero, fmt.Errorf("config file %s: %w", source.Path, err)
		}
		if err := mergeLayer[T](k, layer.Raw(), schema, tag, false); err != nil {
			return zero, fmt.Errorf("config file %s: %w", source.Path, err)
		}
	}
	if opts.Env != nil {
		if err := loadEnv[T](k, schema, tag, *opts.Env); err != nil {
			return zero, err
		}
	}
	var cfg T
	if err := decode(k, &cfg, tag, false); err != nil {
		return zero, fmt.Errorf("decode configuration: %w", err)
	}
	if opts.Validate != nil {
		if err := opts.Validate(cfg); err != nil {
			return zero, fmt.Errorf("validate configuration: %w", err)
		}
	}
	return cfg, nil
}

// checkedTOML retains the TOML decoder's per-file type checking and document
// order for case aliases. Only present keys are projected back into koanf, so
// omitted fields never become zero-value overrides. Other tag namespaces use
// strict mapstructure decoding in mergeLayer instead.
type checkedTOML[T any] struct{ tag string }

//nolint:antislop // koanf.Parser requires dynamic configuration maps at this adapter boundary.
func (p checkedTOML[T]) Unmarshal(data []byte) (map[string]any, error) {
	raw, err := tomlparser.Parser().Unmarshal(data)
	if err != nil || len(raw) == 0 {
		return raw, err
	}
	if p.tag != "toml" {
		return raw, nil
	}
	var cfg T
	if err := toml.Unmarshal(data, &cfg); err != nil {
		var detail *toml.DecodeError
		if errors.As(err, &detail) && len(detail.Key()) > 0 {
			return nil, fmt.Errorf("key %s: %w", strings.Join(detail.Key(), "."), err)
		}
		return nil, err
	}
	typed, err := structs.Provider(cfg, p.tag).Read()
	if err != nil {
		return nil, err
	}
	return project(raw, typed), nil
}

//nolint:antislop // koanf.Parser requires dynamic configuration maps at this adapter boundary.
func (p checkedTOML[T]) Marshal(data map[string]any) ([]byte, error) {
	return tomlparser.Parser().Marshal(data)
}

// project keeps source presence and canonical field names, taking values from
// the already decoded struct. Unknown keys stay unknown for forward compatibility.
//
//nolint:antislop // This helper operates on koanf provider maps; application configuration stays typed.
func project(present, typed map[string]any) map[string]any {
	out := make(map[string]any, len(present))
	for key, value := range present {
		canonical := matchingKey(key, typed)
		if canonical == "" {
			out[key] = value
			continue
		}
		child, ok := value.(map[string]any)
		nested, isMap := typed[canonical].(map[string]any)
		if ok && isMap {
			selected := project(child, nested)
			if previous, exists := out[canonical].(map[string]any); exists {
				for k, v := range selected {
					previous[k] = v
				}
			} else {
				out[canonical] = selected
			}
		} else {
			out[canonical] = typed[canonical]
		}
	}
	return out
}

//nolint:antislop // This helper operates on koanf provider maps; application configuration stays typed.
func matchingKey(key string, schema map[string]any) string {
	if _, ok := schema[key]; ok {
		return key
	}
	for known := range schema {
		if strings.EqualFold(key, known) {
			return known
		}
	}
	return ""
}

// canonicalize prevents a differently cased local key from losing to an exact
// defaults key. Ambiguous aliases in non-TOML providers fail deterministically.
//
//nolint:antislop // This helper adapts koanf provider maps; configuration returned to callers is typed.
func canonicalize(values, schema map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for key, value := range values {
		canonical := matchingKey(key, schema)
		if canonical == "" {
			out[key] = value
			continue
		}
		if _, exists := out[canonical]; exists {
			return nil, fmt.Errorf("duplicate configuration key %s (case-insensitive)", canonical)
		}
		child, ok := value.(map[string]any)
		nested, isMap := schema[canonical].(map[string]any)
		if ok && isMap {
			var err error
			value, err = canonicalize(child, nested)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", canonical, err)
			}
		}
		out[canonical] = value
	}
	return out, nil
}

func decode[T any](k *koanf.Koanf, cfg *T, tag string, parse bool) error {
	dc := &mapstructure.DecoderConfig{WeaklyTypedInput: false, ErrorUnused: false}
	if parse {
		dc.DecodeHook = mapstructure.StringToBasicTypeHookFunc()
	}
	return k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{Tag: tag, DecoderConfig: dc})
}

//nolint:antislop // This helper adapts koanf provider maps; configuration returned to callers is typed.
func mergeLayer[T any](k *koanf.Koanf, values, schema map[string]any, tag string, parse bool) error {
	canonical, err := canonicalize(values, schema)
	if err != nil {
		return err
	}
	layer := koanf.New(".")
	if err := layer.Load(confmap.Provider(canonical, ""), nil); err != nil {
		return err
	}
	var cfg T
	if err := decode(layer, &cfg, tag, parse); err != nil {
		return err
	}
	if parse {
		typed, err := structs.Provider(cfg, tag).Read()
		if err != nil {
			return err
		}
		layer = koanf.New(".")
		if err := layer.Load(confmap.Provider(project(canonical, typed), ""), nil); err != nil {
			return err
		}
	}
	return k.Merge(layer)
}

//nolint:antislop // This helper adapts koanf provider maps; configuration returned to callers is typed.
func loadEnv[T any](k *koanf.Koanf, schema map[string]any, tag string, source EnvSource) error {
	if source.Lookup == nil {
		return errors.New("environment lookup must not be nil")
	}
	names := make([]string, 0, len(source.Variables))
	targets := map[string]bool{}
	for name, key := range source.Variables {
		if name == "" || strings.ContainsAny(name, "=\x00") || key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.Contains(key, "..") || strings.ContainsAny(key, "=\x00") {
			return fmt.Errorf("invalid environment mapping %q -> %q", name, key)
		}
		target := strings.ToLower(key)
		for previous := range targets {
			if target == previous || strings.HasPrefix(target, previous+".") || strings.HasPrefix(previous, target+".") {
				return fmt.Errorf("conflicting environment mapping for %s", name)
			}
		}
		targets[target] = true
		names = append(names, name)
	}
	sort.Strings(names)
	// Map names before passing them to the provider, keeping values as strings
	// and avoiding accidental exposure of unrelated process environment variables.
	environ := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := source.Lookup(name); ok {
			environ = append(environ, source.Variables[name]+"="+value)
		}
	}
	layer := koanf.New(".")
	if err := layer.Load(env.Provider(".", env.Opt{EnvironFunc: func() []string { return environ }}), nil); err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	if err := mergeLayer[T](k, layer.Raw(), schema, tag, source.ParseValues); err != nil {
		return fmt.Errorf("environment (%s): %w", strings.Join(names, ", "), err)
	}
	return nil
}
