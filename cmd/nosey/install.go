package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/storage"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func installCommand(o *cliOptions) *cobra.Command {
	opts := installation.Options{}
	copyMode, yes := false, false
	cmd := &cobra.Command{
		Use:   "install SKILL_ID...",
		Short: "Install catalog skills into this project",
		Long: `Review the whole selection and confirm once in Nosey. All sources then install
without further prompts into this project, replacing existing skills with the
same names, and are recorded in skills-lock.json. Sources run in dependency
order and stop at the first failure; skills already installed are kept and
recorded. In symlink mode, agents whose project directory does not exist are
skipped and reported; --copy installs into every selected agent. --yes skips
the Nosey confirmation.`,
		Example: `  nosey install vercel-labs:agent-skills:react-best-practices
  nosey install vercel-labs:agent-skills:react-best-practices --agent codex --agent claude-code
  nosey install vercel-labs:agent-skills:react-best-practices --agent codex --yes`,
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringSliceVar(&opts.Agents, "agent", nil, "target agent IDs (Nosey asks once when none are set)")
	cmd.Flags().BoolVar(&yes, "yes", false, "install without prompts; requires target agents and replaces existing skills")
	cmd.Flags().BoolVar(&copyMode, "copy", false, "install by copy rather than symlink")
	cmd.Flags().BoolVar(&opts.IgnoreConflicts, "ignore-conflicts", false, "install despite catalog conflicts")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		_, recs, err := s.Scan(cmd.Context(), domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
		if err != nil {
			return err
		}
		s.Inventory(cmd.Context())
		opts.IDs = args
		if copyMode {
			opts.Method = "copy"
		}
		if !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("noninteractive install requires --yes")
		}
		if !yes && len(opts.Agents) == 0 && len(s.Resolved.Settings.Installation.Agents) == 0 {
			opts.Agents, err = selectInstallAgents(cmd, s.Installer)
			if prompts.IsCancel(err) {
				return nil
			}
			if err != nil {
				return err
			}
		}
		opts.Yes = yes
		plans, err := s.Installer.Build(s.Catalog, recs, opts)
		if err != nil {
			return err
		}
		if !yes {
			for _, plan := range plans {
				cmd.Println(plan.Summary())
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
		_, err = s.Installer.Install(cmd.Context(), plans, func(p installation.Progress) {
			if p.Result == nil {
				cmd.Printf("[%d/%d] Installing %s from %s\n", p.Index+1, p.Total, skillNames(p.Plan), p.Plan.Label())
				return
			}
			for _, line := range p.Result.Lines() {
				cmd.Printf("  %s\n", line)
			}
		})
		return fail(5, err)
	}
	return cmd
}

func skillNames(plan installation.SourcePlan) string {
	names := make([]string, 0, len(plan.Skills))
	for _, skill := range plan.Skills {
		names = append(names, storage.Safe(skill.Name))
	}
	return strings.Join(names, ", ")
}

func selectInstallAgents(cmd *cobra.Command, installer installation.Service) ([]string, error) {
	names := installer.AgentNames()
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
