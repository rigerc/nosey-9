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

func updateCommand(o *cliOptions) *cobra.Command {
	opts := installation.UpdateOptions{}
	yes := false
	cmd := &cobra.Command{
		Use:   "update [SKILL_ID...]",
		Short: "Update outdated catalog skills in this project",
		Long: `Refresh installed skills that follow the catalog. A skill is outdated when
the catalog pin differs from the ref recorded in skills-lock.json or when its
upstream content differs from what is installed; the reason is shown for each
skill. With no SKILL_ID, every outdated catalog-managed skill is updated;
otherwise only the named catalog IDs or tracked skill names (skills that are
already current are reported and left alone).

Each skill is refreshed at every location where it is installed in this
project, keeping each location's symlink or copy mode, so no agent directory
is lost. Requirements that the catalog added are installed where the skills
requiring them live. Skills that are not in the catalog are reported as
skipped and never updated. The update needs network access: a failed update
check stops the command and nothing is changed.

Review the whole selection and confirm once in Nosey. Sources then update in
dependency order without further prompts, replacing local edits, and stop at
the first failure; completed updates are kept and recorded in
skills-lock.json. --yes skips the confirmation and is required without a
terminal.`,
		Example: `  nosey update
  nosey update vercel-labs:agent-skills:react-best-practices
  nosey update --yes
  nosey update --ignore-conflicts --yes`,
		Args: cobra.ArbitraryArgs,
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "update without the confirmation prompt; required without a terminal")
	cmd.Flags().BoolVar(&opts.IgnoreConflicts, "ignore-conflicts", false, "update despite catalog conflicts with a newly required skill")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("noninteractive update requires --yes")
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
		opts.IDs = args
		plan, err := s.Installer.PlanUpdate(cmd.Context(), s.Catalog, recs, opts)
		if err != nil {
			return fail(5, err)
		}
		for _, line := range plan.SkippedLines() {
			cmd.Println(line)
		}
		if plan.Empty() {
			cmd.Println("Nothing to update.")
			return nil
		}
		if !yes {
			for _, line := range plan.Lines() {
				cmd.Println(line)
			}
			cmd.Println("Scope: project. Local edits to these skills will be replaced.")
			approved, err := confirm(cmd, fmt.Sprintf("Update %s without further prompts?", skillCountText(plan.Count())))
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
		results, err := s.Installer.Update(cmd.Context(), plan, func(p installation.Progress) {
			if p.Result == nil {
				cmd.Printf("[%d/%d] Updating %s from %s\n", p.Index+1, p.Total, skillNames(p.Plan), p.Plan.Label())
				return
			}
			for _, line := range p.Result.Lines() {
				cmd.Printf("  %s\n", line)
			}
		})
		for _, r := range results {
			if !r.Started {
				for _, line := range r.Lines() {
					cmd.Printf("  %s\n", line)
				}
			}
		}
		return fail(5, err)
	}
	return cmd
}

func skillCountText(n int) string {
	if n == 1 {
		return "1 skill"
	}
	return fmt.Sprintf("%d skills", n)
}
