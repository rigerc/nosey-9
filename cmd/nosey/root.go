package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/bueti/skillgen"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rigerc/nosey-9/internal/config"
	"github.com/rigerc/nosey-9/internal/domain"
	pkg "github.com/rigerc/nosey-9/internal/package"
	"github.com/rigerc/nosey-9/internal/ui"
	"github.com/rigerc/nosey-9/internal/utils"
)

// Metadata loaded from package.toml at build time
var (
	version = pkg.Version()
	name    = pkg.Name()
	short   = pkg.Short()
)

type rootOptions struct {
	configPath  string
	showVersion bool
}

var rootCmd = newRootCmd()

// Execute is the CLI entrypoint.
func Execute() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return rootCmd.ExecuteContext(ctx)
}

func newDemoRootCmd() *cobra.Command {
	opts := &rootOptions{}
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.showVersion {
				ver := resolvedVersion()
				cmd.Printf("%s\n", ver)
				return nil
			}
			// Interactive file manager will go here
			return runInteractive(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.configPath, "config", "c", "", "config file path")

	cmd.Flags().BoolVarP(&opts.showVersion, "version", "v", false, "print version information")

	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newGreetCmd())
	cmd.AddCommand(newPromptCmd())
	cmd.AddCommand(newCompletionCmd())
	cmd.AddCommand(newSkillsCmd(cmd))

	return cmd
}

// skillgen uses the Cobra command tree directly, including aliases and flags.
// The name override retains make skills SKILL_NAME=... for downstream CLIs.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func newSkillsCmd(root *cobra.Command) *cobra.Command {
	skills := skillgen.NewSkillsCmd(root)
	var skillName string
	skills.PersistentFlags().StringVar(&skillName, "skill-name", "", "override the generated skill name")
	skills.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if skillName == "" {
			return nil
		}
		if !skillNamePattern.MatchString(skillName) {
			return fmt.Errorf("invalid skill name %q: use lowercase letters, numbers, and interior hyphens", skillName)
		}
		if root.Annotations == nil {
			root.Annotations = make(map[string]string)
		}
		root.Annotations[skillgen.AnnotationName] = skillName
		return nil
	}
	return skills
}

func resolvedVersion() string {
	ver := version
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ver
	}
	if ver == "dev" && strings.TrimSpace(info.Main.Version) != "" && info.Main.Version != "(devel)" {
		ver = info.Main.Version
	}
	return ver
}

func runInteractive(opts *rootOptions) error {
	if err := requireInteractive(os.Stdin, os.Stdout); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	cfg, err := loadRootConfig(cwd, opts)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	return runDirectoryListing(cwd, cfg)
}

func loadRootConfig(cwd string, opts *rootOptions) (domain.Config, error) {
	manager := config.NewManager(cwd)
	if opts.configPath != "" {
		return manager.LoadWithOverride(opts.configPath)
	}
	return manager.Load()
}

// requireInteractive reports whether both input and output are attached to a
// terminal. Bubble Tea needs a TTY for key input and cursor control.
func requireInteractive(in, out *os.File) error {
	if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return errors.New("interactive terminal required: run this command from a terminal")
	}
	return nil
}

func runDirectoryListing(cwd string, cfg domain.Config) error {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	items := make([]list.Item, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, fileItem{
			name:    entry.Name(),
			isDir:   entry.IsDir(),
			modTime: info.ModTime(),
		})
	}

	if len(items) == 0 {
		fmt.Println("Directory is empty")
		return nil
	}

	theme := ui.ThemeFromConfig(cfg)
	delegate := ui.NewListDelegate(theme, ui.ListDelegateOptions{
		Spacing: cfg.ListSpacing,
	})

	listModel := ui.NewListModel(items, delegate, 80, 20, theme)
	listModel.Title = fmt.Sprintf("Directory: %s", cwd)
	listModel.SetShowStatusBar(true)
	listModel.SetFilteringEnabled(true)
	listModel.DisableQuitKeybindings()

	model := directoryListModel{
		list:       listModel,
		theme:      theme,
		responsive: ui.NewResponsiveManager(80),
	}

	// Set initial keybindings based on initial screen size
	model.list.AdditionalShortHelpKeys = model.getShortHelpKeys
	model.list.AdditionalFullHelpKeys = model.getFullHelpKeys

	p := tea.NewProgram(model, tea.WithoutSignalHandler())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run interactive list: %w", err)
	}

	return nil
}

type fileItem struct {
	name    string
	isDir   bool
	modTime time.Time
}

func (f fileItem) Title() string {
	if f.isDir {
		return f.name + "/"
	}
	return f.name
}

func (f fileItem) Description() string {
	return utils.TimeAgo(f.modTime)
}

func (f fileItem) FilterValue() string {
	return f.name
}

type directoryListModel struct {
	list       list.Model
	theme      ui.Theme
	responsive *ui.ResponsiveManager
	message    string
}

// allHelpKeys returns the complete list of keybindings in priority order.
func (m directoryListModel) allHelpKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "inspect selected entry"),
		),
		key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
	}
}

// getShortHelpKeys returns keybindings for short help based on screen size
func (m directoryListModel) getShortHelpKeys() []key.Binding {
	allKeys := m.allHelpKeys()

	var splitAt int
	switch m.responsive.Breakpoint() {
	case ui.BreakpointXL:
		splitAt = 3
	case ui.BreakpointLG:
		splitAt = 1
	default:
		splitAt = 1
	}

	return allKeys[:min(splitAt, len(allKeys))]
}

// getFullHelpKeys returns keybindings for full help
func (m directoryListModel) getFullHelpKeys() []key.Binding {
	allKeys := m.allHelpKeys()

	// Determine split point based on breakpoint
	var splitAt int
	switch m.responsive.Breakpoint() {
	case ui.BreakpointXS:
		splitAt = 1 // Remaining keys after first
	case ui.BreakpointSM:
		splitAt = 2 // Remaining keys after first 2
	case ui.BreakpointMD:
		splitAt = 3 // Remaining keys after first 3
	default:
		// LG and XL: all shown in short, return empty for full
		return []key.Binding{}
	}

	return allKeys[min(splitAt, len(allKeys)):]
}

func (m directoryListModel) Init() tea.Cmd {
	return nil
}

func (m directoryListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Update responsive manager with new width
		m.responsive.SetWidth(msg.Width)

		// Get responsive list dimensions
		width, height := m.responsive.GetListDimensions(msg.Width, msg.Height)
		m.list.SetSize(width, height)

		// Update keybindings based on new screen size
		m.list.AdditionalShortHelpKeys = m.getShortHelpKeys
		m.list.AdditionalFullHelpKeys = m.getFullHelpKeys
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// Let the list receive q while the user is typing a filter.
			if m.list.SettingFilter() {
				break
			}
			return m, tea.Quit
		case "esc":
			// Esc cancels or clears a filter; outside filtering it exits.
			if m.list.SettingFilter() || m.list.IsFiltered() {
				break
			}
			return m, tea.Quit
		case "enter":
			// Enter applies an active filter; otherwise it inspects the entry.
			if m.list.SettingFilter() {
				break
			}
			if item, ok := m.list.SelectedItem().(fileItem); ok {
				m.message = fmt.Sprintf("Selected: %s", item.name)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m directoryListModel) View() tea.View {
	listView := m.list.View()

	// Add message if present
	if m.message != "" {
		listView = listView + "\n\n" + m.message
	}

	return tea.NewView(m.responsive.AdaptiveFrameStyle(m.theme).Render(listView))
}
