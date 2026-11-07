// Package icon provides Nerd Font icons used by the UI.
package icon

// Icon represents a nerd font icon.
type Icon string

const (
	// Bookmarks is the bookmarks icon.
	Bookmarks Icon = "\uf097"

	// Tmux is the tmux icon.
	Tmux Icon = "\uebc8"

	// Nvim is the Neovim icon.
	Nvim Icon = "\uf36f"

	// Vim is the Vim icon.
	Vim Icon = "\uf36f"

	// File is the generic file icon.
	File Icon = "\uf15b"

	// Script is the script/terminal icon.
	Script Icon = "\ue691"

	// Shell is the shell icon.
	Shell Icon = "\uf489"
)

// Get returns the icon as a string
func (i Icon) String() string {
	return string(i)
}

// GetEditorIcon returns the appropriate icon for a given editor
func GetEditorIcon(editor string) Icon {
	switch editor {
	case "nvim", "neovim":
		return Nvim
	case "vim":
		return Vim
	default:
		return ""
	}
}
