package main

import (
	"fmt"
	"os"

	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func uninstallCommand(o *cliOptions) *cobra.Command {
	opts := installation.UninstallOptions{}
	yes := false
	cmd := &cobra.Command{
		Use:   "uninstall NAME_OR_ID...",
		Short: "Remove skills tracked in skills-lock.json from this project",
		Long: `Remove skills from every agent directory of this project and delete their
skills-lock.json entries. Targets are tracked skill names or catalog IDs; a
skill that skills-lock.json does not track, or that is unknown, is refused and
nothing is removed. Skills outside the selection are never touched.

The whole selection is evaluated together. Removal is blocked while a skill
that stays installed requires one of the targets (through catalog requirements
or recommendation mappings) unless --ignore-requirements is passed; selecting
a skill together with what it requires is allowed, and dependents are removed
first. Removal stops at the first failure and reports what was removed and what
was not started; a skill that could not be fully removed keeps its lock entry.

Review the selection and confirm once in Nosey. --yes skips the confirmation
and is required without a terminal.`,
		Example: `  nosey uninstall react-best-practices
  nosey uninstall vercel-labs:agent-skills:react-best-practices --yes
  nosey uninstall web-design-guidelines react-best-practices --yes
  nosey uninstall react-best-practices --ignore-requirements --yes`,
		Args: cobra.MinimumNArgs(1),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "remove without the confirmation prompt; required without a terminal")
	cmd.Flags().BoolVar(&opts.IgnoreRequirements, "ignore-requirements", false, "remove even when installed skills still require a target")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("noninteractive uninstall requires --yes")
		}
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		_, recs, err := s.Scan(cmd.Context(), domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
		if err != nil {
			return err
		}
		if err = s.Installer.Prepare(cmd.Context()); err != nil {
			return err
		}
		opts.Targets = args
		plan, err := s.Installer.PlanUninstall(cmd.Context(), s.Catalog, recs, opts)
		if err != nil {
			return fail(5, err)
		}
		if !yes {
			for _, line := range plan.Lines() {
				cmd.Println(line)
			}
			cmd.Println("Scope: project. Files are deleted from every agent directory.")
			approved, err := confirm(cmd, fmt.Sprintf("Remove %s without further prompts?", skillCountText(len(plan.Skills))))
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
		results, err := s.Installer.Uninstall(cmd.Context(), plan, func(index, total int, result *installation.RemoveResult) {
			if result == nil {
				cmd.Printf("[%d/%d] Removing %s\n", index+1, total, plan.Skills[index].Name)
				return
			}
			cmd.Printf("  %s\n", result.Line())
		})
		for _, r := range results {
			if r.Status == installation.RemoveNotStarted {
				cmd.Printf("  %s\n", r.Line())
			}
		}
		return fail(5, err)
	}
	return cmd
}
