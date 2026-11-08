package config_test

import (
	"fmt"

	"github.com/rigerc/nosey-9/internal/config"
)

func ExampleLoad() {
	// These settings belong to the downstream application, not the UI demo.
	type Settings struct {
		Endpoint string `toml:"endpoint"`
		Retries  int    `toml:"retries"`
	}
	lookup := func(name string) (string, bool) {
		value, ok := map[string]string{"MYCLI_RETRIES": "5"}[name]
		return value, ok
	}
	cfg, err := config.Load(Settings{Endpoint: "https://api.example.com", Retries: 3}, config.Options[Settings]{
		Env: &config.EnvSource{
			Variables: map[string]string{"MYCLI_ENDPOINT": "endpoint", "MYCLI_RETRIES": "retries"},
			Lookup:    lookup, ParseValues: true,
		},
		Validate: func(cfg Settings) error {
			if cfg.Endpoint == "" {
				return fmt.Errorf("endpoint must not be empty")
			}
			if cfg.Retries < 0 {
				return fmt.Errorf("retries must be nonnegative")
			}
			return nil
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s retries=%d\n", cfg.Endpoint, cfg.Retries)
	// Output: https://api.example.com retries=5
}
