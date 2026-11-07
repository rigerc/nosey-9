package main

import "testing"

func TestDemosAreComplete(t *testing.T) {
	seen := make(map[string]struct{})
	for _, d := range allDemos() {
		if d.name == "" {
			t.Error("demo has an empty name")
		}
		if d.summary == "" {
			t.Errorf("demo %q has an empty summary", d.name)
		}
		if d.run == nil {
			t.Errorf("demo %q has no run function", d.name)
		}
		if _, exists := seen[d.name]; exists {
			t.Errorf("demo %q is registered more than once", d.name)
		}
		seen[d.name] = struct{}{}
	}
}
