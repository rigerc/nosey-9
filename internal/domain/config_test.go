package domain

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	want := Config{
		Editor:               "nvim",
		Headings:             "15",
		Primary:              "02",
		Secondary:            "06",
		Text:                 "07",
		TextHighlight:        "06",
		DescriptionHighlight: "05",
		Tags:                 "13",
		Flags:                "12",
		Muted:                "08",
		Accent:               "13",
		Border:               "08",
		ListSpacing:          "space",
	}

	if got := DefaultConfig(); got != want {
		t.Errorf("DefaultConfig() = %+v, want %+v", got, want)
	}
}

func TestDefaultConfig_Consistency(t *testing.T) {
	t.Run("multiple calls return same values", func(t *testing.T) {
		cfg1 := DefaultConfig()
		cfg2 := DefaultConfig()

		if cfg1 != cfg2 {
			t.Errorf("DefaultConfig() calls differ: %+v != %+v", cfg1, cfg2)
		}
	})
}
