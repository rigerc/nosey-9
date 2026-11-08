package prompts

import "github.com/rigerc/nosey-9/internal/clack/core"

// PersistentComponent renders one block above the current form.
type PersistentComponent = core.PersistentComponent

// StaticComponent creates a persistent block from fixed text, such as a banner.
func StaticComponent(text string) PersistentComponent {
	return func() string { return text }
}

// SetPersistent replaces the components displayed above each interactive form.
// Existing forms are replaced as subsequent prompts start, keeping the header
// in place. The session supports one active interactive form at a time.
func SetPersistent(components ...PersistentComponent) {
	core.SetPersistent(components...)
}

// RefreshPersistent requests a redraw of the current form and its header.
func RefreshPersistent() {
	core.RefreshPersistent()
}

// ClearPersistent stops displaying persistent components on future forms.
func ClearPersistent() {
	core.ClearPersistent()
}
