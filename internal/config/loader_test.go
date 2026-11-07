package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type appSettings struct {
	Endpoint string         `toml:"endpoint"`
	Retries  int            `toml:"retries"`
	Enabled  bool           `toml:"enabled"`
	Labels   []string       `toml:"labels"`
	Server   serverSettings `toml:"server"`
}
type serverSettings struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

func appDefaults() appSettings {
	return appSettings{Endpoint: "https://example.com", Retries: 3, Enabled: true, Labels: []string{"default"}, Server: serverSettings{Host: "localhost", Port: 8080}}
}
func configFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func envLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) { value, ok := values[name]; return value, ok }
}

func TestLoadApplicationSettings(t *testing.T) {
	dir := t.TempDir()
	global := configFile(t, dir, "global.toml", "endpoint = 'global'\nretries = 5\n[server]\nport = 9000\n")
	local := configFile(t, dir, "local.toml", "ENDPOINT = ''\nenabled = false\nlabels = []\n[SERVER]\nHOST = 'local'\n")
	defaults := appDefaults()
	cfg, err := Load(defaults, Options[appSettings]{Files: []FileSource{{Path: filepath.Join(dir, "missing"), Optional: true}, {Path: global}, {Path: local}}, Env: &EnvSource{Variables: map[string]string{"APP_HOST": "server.host", "APP_RETRIES": "retries"}, Lookup: envLookup(map[string]string{"APP_HOST": "env", "APP_RETRIES": "7", "OTHER": "ignore"}), ParseValues: true}, Validate: func(cfg appSettings) error {
		if cfg.Retries < 1 {
			return errors.New("retries must be positive")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != "" || cfg.Enabled || len(cfg.Labels) != 0 || cfg.Retries != 7 || cfg.Server.Host != "env" || cfg.Server.Port != 9000 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if defaults.Labels[0] != "default" || defaults.Server.Port != 8080 {
		t.Fatal("defaults were modified")
	}
	cfg.Labels = append(cfg.Labels, "new")
	next, err := Load(defaults, Options[appSettings]{})
	if err != nil {
		t.Fatal(err)
	}
	if next.Endpoint != defaults.Endpoint || len(next.Labels) != 1 || next.Labels[0] != "default" {
		t.Fatalf("load retained state: %+v", next)
	}
}
func TestLoadSourceErrors(t *testing.T) {
	for _, tc := range []struct{ name, body, key string }{
		{"string integer", "endpoint = 2", "endpoint"}, {"string bool", "endpoint = false", "endpoint"}, {"string array", "endpoint = []", "endpoint"}, {"string table", "endpoint = {}", "endpoint"}, {"int string", "retries = '2'", "retries"}, {"bool string", "enabled = 'true'", "enabled"}, {"nested type", "[server]\nport = '9000'", "port"}, {"malformed", "endpoint = [", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bad := configFile(t, dir, "bad.toml", tc.body)
			good := configFile(t, dir, "good.toml", "endpoint = 'valid'\nretries = 4\nenabled = true\n[server]\nport = 9000\n")
			_, err := Load(appDefaults(), Options[appSettings]{Files: []FileSource{{Path: bad}, {Path: good}}})
			if err == nil || !strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected source/key error, got %v", err)
			}
		})
	}
}
func TestLoadUnknownAndCaseAliases(t *testing.T) {
	dir := t.TempDir()
	path := configFile(t, dir, "case.toml", "ENDPOINT = 'first'\nendpoint = 'second'\nEndpoint = 'last'\ninteractive_default = false\n[unused]\nx = true\n")
	cfg, err := Load(appDefaults(), Options[appSettings]{Files: []FileSource{{Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != "last" {
		t.Fatalf("document order lost: %+v", cfg)
	}
	path = configFile(t, dir, "bad-case.toml", "ENDPOINT = 5\nendpoint = 'valid'\n")
	if _, err := Load(appDefaults(), Options[appSettings]{Files: []FileSource{{Path: path}}}); err == nil {
		t.Fatal("later case alias hid a type error")
	}
}
func TestLoadEnvironmentPolicy(t *testing.T) {
	t.Setenv("APP_ENDPOINT", "process value must not leak")
	cfg, err := Load(appDefaults(), Options[appSettings]{Env: &EnvSource{Variables: map[string]string{"APP_ENDPOINT": "endpoint"}, Lookup: envLookup(nil)}})
	if err != nil || cfg.Endpoint != appDefaults().Endpoint {
		t.Fatalf("process environment leaked: %+v %v", cfg, err)
	}
	cfg, err = Load(appDefaults(), Options[appSettings]{Env: &EnvSource{Variables: map[string]string{"APP_ENDPOINT": "endpoint"}, Lookup: envLookup(map[string]string{"APP_ENDPOINT": "a=b"})}})
	if err != nil || cfg.Endpoint != "a=b" {
		t.Fatalf("value with equals lost: %+v %v", cfg, err)
	}
	cfg, err = Load(appDefaults(), Options[appSettings]{Env: &EnvSource{Variables: map[string]string{"APP_ENDPOINT": "endpoint"}, Lookup: envLookup(map[string]string{"APP_ENDPOINT": ""})}})
	if err != nil || cfg.Endpoint != "" {
		t.Fatalf("empty value lost: %+v %v", cfg, err)
	}
	for _, parse := range []bool{false, true} {
		_, err := Load(appDefaults(), Options[appSettings]{Env: &EnvSource{Variables: map[string]string{"APP_RETRIES": "retries"}, Lookup: envLookup(map[string]string{"APP_RETRIES": "invalid"}), ParseValues: parse}})
		if err == nil || !strings.Contains(err.Error(), "APP_RETRIES") || !strings.Contains(err.Error(), "retries") {
			t.Fatalf("expected environment/key error, got %v", err)
		}
	}
	type colorSettings struct {
		Color string `toml:"color"`
	}
	color, err := Load(colorSettings{Color: "13"}, Options[colorSettings]{Env: &EnvSource{Variables: map[string]string{"COLOR": "color"}, Lookup: envLookup(map[string]string{"COLOR": "02"}), ParseValues: true}})
	if err != nil || color.Color != "02" {
		t.Fatalf("numeric string changed: %+v %v", color, err)
	}
}
func TestLoadMissingSourcesAndValidation(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")
	for _, path := range []string{missing, dir} {
		if _, err := Load(appDefaults(), Options[appSettings]{Files: []FileSource{{Path: path}}}); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("required source accepted: %v", err)
		}
		if _, err := Load(appDefaults(), Options[appSettings]{Files: []FileSource{{Path: path, Optional: true}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Load(appDefaults(), Options[appSettings]{Files: []FileSource{{}}}); err == nil {
		t.Fatal("accepted empty path")
	}
	sentinel := errors.New("bad application settings")
	_, err := Load(appDefaults(), Options[appSettings]{Validate: func(appSettings) error { return sentinel }})
	if !errors.Is(err, sentinel) {
		t.Fatalf("validation error lost: %v", err)
	}
	if _, err := Load(3, Options[int]{}); err == nil {
		t.Fatal("accepted non-struct defaults")
	}
	var ptr *appSettings
	if _, err := Load(ptr, Options[*appSettings]{}); err == nil {
		t.Fatal("accepted nil defaults")
	}
}
func TestLoadCustomTags(t *testing.T) {
	type custom struct {
		URL string `settings:"api_url"`
	}
	dir := t.TempDir()
	path := configFile(t, dir, "custom.toml", "API_URL = 'custom'\n")
	cfg, err := Load(custom{URL: "default"}, Options[custom]{Tag: "settings", Files: []FileSource{{Path: path}}})
	if err != nil || cfg.URL != "custom" {
		t.Fatalf("custom tags: %+v %v", cfg, err)
	}
	bad := configFile(t, dir, "bad.toml", "api_url = 2\n")
	if _, err := Load(custom{}, Options[custom]{Tag: "settings", Files: []FileSource{{Path: bad}}}); err == nil {
		t.Fatal("custom tags allowed weak conversion")
	}
}
func TestLoadInvalidEnvironmentMappings(t *testing.T) {
	for _, variables := range []map[string]string{{"": "endpoint"}, {"A=B": "endpoint"}, {"A": ""}, {"A": ".endpoint"}, {"A": "endpoint", "B": "ENDPOINT"}, {"A": "server", "B": "server.host"}} {
		if _, err := Load(appDefaults(), Options[appSettings]{Env: &EnvSource{Variables: variables, Lookup: envLookup(nil)}}); err == nil {
			t.Fatalf("accepted mapping %v", variables)
		}
	}
	if _, err := Load(appDefaults(), Options[appSettings]{Env: &EnvSource{}}); err == nil {
		t.Fatal("accepted nil lookup")
	}
}
func TestLoadConcurrent(t *testing.T) {
	for range 8 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(appDefaults(), Options[appSettings]{})
			if err != nil || cfg.Server.Port != 8080 {
				t.Fatalf("%+v %v", cfg, err)
			}
		})
	}
}

func TestLoadEnvironmentConversionIsOptIn(t *testing.T) {
	source := &EnvSource{Variables: map[string]string{"RETRIES": "retries"}, Lookup: envLookup(map[string]string{"RETRIES": "7"})}
	if _, err := Load(appDefaults(), Options[appSettings]{Env: source}); err == nil {
		t.Fatal("converted environment input without ParseValues")
	}
	source.ParseValues = true
	cfg, err := Load(appDefaults(), Options[appSettings]{Env: source})
	if err != nil || cfg.Retries != 7 {
		t.Fatalf("opt-in conversion failed: %+v %v", cfg, err)
	}
}
