package main

import (
	"fmt"
	"os"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func installCommand(o *cliOptions) *cobra.Command {
	opts := installation.Options{}
	printMode, copyMode := false, false
	cmd := &cobra.Command{
		Use:   "install SKILL_ID...",
		Short: "Install catalog skills with the upstream skills CLI",
		Long: `Install catalog skills with the upstream skills CLI, one source at a time.

The skills CLI shows what it will change and asks for confirmation, including
installation scope and any existing skills it would overwrite. With --yes it
installs into this project without asking and replaces existing skills.`,
		Example: `  nosey install vercel-labs:agent-skills:react-best-practices
  nosey install vercel-labs:agent-skills:react-best-practices --agent codex --agent claude-code
  nosey install vercel-labs:agent-skills:react-best-practices --agent codex --yes
  nosey install vercel-labs:agent-skills:react-best-practices --print`,
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringSliceVar(&opts.Agents, "agent", nil, "target agent IDs (the skills CLI asks when none are set)")
	cmd.Flags().BoolVar(&printMode, "print", false, "print the commands without running the skills CLI")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "install without prompts; requires target agents and replaces existing skills")
	cmd.Flags().BoolVar(&copyMode, "copy", false, "install by copy rather than symlink")
	cmd.Flags().BoolVar(&opts.IgnoreConflicts, "ignore-conflicts", false, "install despite catalog conflicts")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		if !opts.Yes && s.Resolved.Settings.Installation.Mode == "print" {
			printMode = true
		}
		_, recs, err := s.Scan(cmd.Context(), domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
		if err != nil {
			return err
		}
		if !printMode {
			s.Inventory(cmd.Context())
		}
		opts.IDs = args
		if copyMode {
			opts.Method = "copy"
		}
		commands, err := s.Installer.Build(s.Catalog, recs, opts)
		if err != nil {
			return err
		}
		if printMode {
			for _, command := range commands {
				cmd.Println(command.Text())
			}
			return nil
		}
		if !opts.Yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("noninteractive install requires --yes")
		}
		return fail(5, s.Installer.Install(cmd.Context(), commands))
	}
	return cmd
}
