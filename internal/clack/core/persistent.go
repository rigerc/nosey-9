package core

import (
	"os"
	"strings"
	"sync"
)

// PersistentComponent produces a styled go-clack header. The header is
// rendered as part of the Bubble Tea view, never written on a side channel.
type PersistentComponent func() string

var persistent = struct {
	sync.RWMutex
	components   []PersistentComponent
	output       *os.File
	activeRender func()
}{}

// SetPersistent installs reusable content above prompts.
func SetPersistent(components ...PersistentComponent) {
	persistent.Lock()
	persistent.components = append([]PersistentComponent(nil), components...)
	refresh := persistent.activeRender
	persistent.Unlock()
	if refresh != nil {
		refresh()
	}
}

// ClearPersistent removes persistent content.
func ClearPersistent() { SetPersistent() }

// RefreshPersistent redraws persistent content.
func RefreshPersistent() {
	persistent.RLock()
	refresh := persistent.activeRender
	persistent.RUnlock()
	if refresh != nil {
		refresh()
	}
}

func beginPersistent(output *os.File, refresh func()) {
	persistent.Lock()
	persistent.output = output
	persistent.activeRender = refresh
	persistent.Unlock()
}

func endPersistent(output *os.File) {
	persistent.Lock()
	if persistent.output == output {
		persistent.output = nil
		persistent.activeRender = nil
	}
	persistent.Unlock()
}

// PersistentHeader returns the themed shared header for composed models.
func PersistentHeader() string {
	persistent.RLock()
	snapshot := append([]PersistentComponent(nil), persistent.components...)
	persistent.RUnlock()
	return renderPersistent(snapshot)
}

func persistentHeaderFor(output *os.File) string {
	persistent.RLock()
	active := persistent.output == output
	snapshot := append([]PersistentComponent(nil), persistent.components...)
	persistent.RUnlock()
	if !active {
		return ""
	}
	return renderPersistent(snapshot)
}

func renderPersistent(components []PersistentComponent) string {
	parts := make([]string, 0, len(components))
	for _, component := range components {
		if component == nil {
			continue
		}
		content := strings.TrimRight(strings.ReplaceAll(component(), "\r\n", "\n"), "\n")
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

// SetPersistentForWorkflow attaches the persistent header to a composed program.
func SetPersistentForWorkflow(output *os.File, refresh func()) { beginPersistent(output, refresh) }

// EndPersistentForWorkflow releases the refresh hook after a program ends.
func EndPersistentForWorkflow(output *os.File) { endPersistent(output) }
