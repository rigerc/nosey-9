package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func installCommand(o *cliOptions) *cobra.Command {
	opts := installation.Options{}
	dry, printMode, yes, jsonMode, copyMode, verbose := false, false, false, false, false, false
	approval := ""
	cmd := &cobra.Command{Use: "install [SKILL_ID...]", Short: "Review and install selected project skills"}
	cmd.Flags().StringSliceVar(&opts.Agents, "agent", nil, "explicit target agent IDs")
	cmd.Flags().StringVar(&opts.Source, "source", "", "direct uncatalogued Git URI or absolute local path")
	cmd.Flags().StringVar(&opts.SkillName, "skill", "", "upstream skill name for a direct source")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "plan without invoking the installer")
	cmd.Flags().BoolVar(&printMode, "print", false, "print shell-escaped commands without invoking the installer")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the reviewed project installation")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit dry-run plan JSON v3 (requires --dry-run)")
	cmd.Flags().BoolVar(&copyMode, "copy", false, "install by copy rather than symlink")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "show installation progress")
	cmd.Flags().BoolVar(&opts.IgnoreConflicts, "ignore-conflicts", false, "explicitly ignore displayed conflicts")
	cmd.Flags().StringVar(&approval, "approve-plan", "", "additional reviewed-plan SHA-256 approval")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if dry && printMode || jsonMode && !dry {
			return fmt.Errorf("--json requires --dry-run; --dry-run and --print are exclusive")
		}
		var s app.Session
		var err error
		if opts.Source != "" {
			s, err = o.configuredSession(cmd.Context())
		} else {
			s, err = o.session(cmd.Context(), false)
		}
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("verbose") {
			verbose = s.Resolved.Settings.Installation.Verbose
		}
		opts.IDs = args
		if copyMode {
			opts.Method = "copy"
		}
		var recs catalog.Recommendations
		if opts.Source == "" {
			_, recs, err = s.Scan(cmd.Context(), domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
			if err != nil {
				return err
			}
		}
		p, err := s.Installer.Plan(cmd.Context(), s.Catalog, recs, opts)
		if err != nil {
			return err
		}
		if dry {
			if jsonMode {
				return emit(cmd, "install-plan-output.v3.schema.json", p)
			}
			if ready := p.CommandsReady(); ready != nil {
				for _, item := range p.Items {
					cmd.Printf("Plan: %s from %s (%s; agents %s)\n", item.SkillName, item.LogicalSource, item.Method, strings.Join(item.Agents, ", "))
				}
				cmd.Println(ready.Error())
			} else {
				for _, command := range p.Commands {
					cmd.Println(shellCommand(command.Argv))
				}
			}
			if p.Approval.Digest != nil {
				cmd.Printf("Approval digest: %s\n", *p.Approval.Digest)
			}
			return nil
		}
		if !dry && !yes && s.Resolved.Settings.Installation.Mode == "print" {
			printMode = true
		}
		if printMode {
			if err = p.CommandsReady(); err != nil {
				return err
			}
			for _, command := range p.Commands {
				cmd.Println(shellCommand(command.Argv))
			}
			return nil
		}
		if !yes {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("noninteractive install requires --yes")
			}
			for _, command := range p.Commands {
				cmd.Println(shellCommand(command.Argv))
			}
			confirmed, err := confirm(cmd, "Install these selected skills in this project?")
			if err != nil {
				return err
			}
			yes = confirmed
		}
		if !yes {
			return nil
		}
		if verbose {
			cmd.PrintErrln("Executing confirmed project installation")
		}
		return fail(5, s.Installer.Execute(cmd.Context(), p, approval, true))
	}
	return cmd
}
func shellCommand(argv []string) string {
	parts := []string{}
	for _, arg := range argv {
		parts = append(parts, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
	}
	return strings.Join(parts, " ")
}
