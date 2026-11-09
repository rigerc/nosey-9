package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/storage"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func installCommand(o *cliOptions) *cobra.Command {
	opts := installation.Options{}
	printMode, copyMode, yes := false, false, false
	cmd := &cobra.Command{
		Use:   "install SKILL_ID...",
		Short: "Install catalog skills with the upstream skills CLI",
		Long: `Review the whole selection and confirm once in Nosey. All sources then run
without further prompts, installing into this project and replacing existing
skills with the same names. --yes skips the Nosey confirmation.`,
		Example: `  nosey install vercel-labs:agent-skills:react-best-practices
  nosey install vercel-labs:agent-skills:react-best-practices --agent codex --agent claude-code
  nosey install vercel-labs:agent-skills:react-best-practices --agent codex --yes
  nosey install vercel-labs:agent-skills:react-best-practices --print`,
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringSliceVar(&opts.Agents, "agent", nil, "target agent IDs (Nosey asks once when none are set)")
	cmd.Flags().BoolVar(&printMode, "print", false, "print the commands without running the skills CLI")
	cmd.Flags().BoolVar(&yes, "yes", false, "install without prompts; requires target agents and replaces existing skills")
	cmd.Flags().BoolVar(&copyMode, "copy", false, "install by copy rather than symlink")
	cmd.Flags().BoolVar(&opts.IgnoreConflicts, "ignore-conflicts", false, "install despite catalog conflicts")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		if !yes && s.Resolved.Settings.Installation.Mode == "print" {
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
		if !printMode && !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("noninteractive install requires --yes")
		}
		if !printMode && !yes && len(opts.Agents) == 0 && len(s.Resolved.Settings.Installation.Agents) == 0 {
			opts.Agents, err = selectInstallAgents(cmd)
			if prompts.IsCancel(err) {
				return nil
			}
			if err != nil {
				return err
			}
		}
		opts.Yes = !printMode || len(opts.Agents) > 0 || len(s.Resolved.Settings.Installation.Agents) > 0
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
		if !yes {
			for _, command := range commands {
				cmd.Println(storage.Safe(command.Text()))
			}
			cmd.Println("Scope: project. Existing skills with the same names will be replaced.")
			approved, err := confirm(cmd, "Install the entire selection without further prompts?")
			if prompts.IsCancel(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if !approved {
				return nil
			}
		}
		return fail(5, s.Installer.Install(cmd.Context(), commands))
	}
	return cmd
}

func selectInstallAgents(cmd *cobra.Command) ([]string, error) {
	names := installation.AgentNames()
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	options := make([]*prompts.MultiSelectOption[string], 0, len(ids))
	for _, id := range ids {
		options = append(options, &prompts.MultiSelectOption[string]{Value: id, Label: names[id] + " (" + id + ")"})
	}
	selected, err := prompts.MultiSelect(prompts.MultiSelectParams[string]{Context: cmd.Context(), Message: "Target agents for the entire selection", Options: options, Required: true, Filter: true, Input: os.Stdin, Output: os.Stderr})
	return selected, err
}
