package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/clack/prompts"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"github.com/rigerc/nosey-9/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type commandError struct {
	code int
	err  error
}

func (e commandError) Error() string { return e.err.Error() }
func (e commandError) Unwrap() error { return e.err }
func exitCode(err error) int {
	var missing *exec.Error
	if errors.As(err, &missing) {
		return 6
	}
	var policy installation.PolicyError
	if errors.As(err, &policy) {
		return 2
	}
	var prerequisite installation.PrerequisiteError
	if errors.As(err, &prerequisite) {
		return 6
	}
	var typed commandError
	if errors.As(err, &typed) {
		return typed.code
	}
	return 2
}
func fail(code int, err error) error {
	if err == nil {
		return nil
	}
	return commandError{code, err}
}

type cliOptions struct {
	cwd, config string
	open        func(context.Context, string, string, bool) (app.Session, error)
}

func (o *cliOptions) store() (settings.Store, error) {
	cwd := o.cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return settings.Store{}, err
		}
	}
	return settings.New(cwd, o.config)
}
func (o *cliOptions) session(ctx context.Context, refresh bool) (app.Session, error) {
	store, err := o.store()
	if err != nil {
		return app.Session{}, err
	}
	if _, resolveErr := store.Resolve(); resolveErr != nil {
		return app.Session{}, resolveErr
	}
	open := o.open
	if open == nil {
		open = app.Open
	}
	s, err := open(ctx, store.Workspace, o.config, refresh)
	if err != nil {
		return s, fail(3, err)
	}
	return s, nil
}
func (o *cliOptions) configuredSession(ctx context.Context) (app.Session, error) {
	if o.open != nil {
		return o.session(ctx, false)
	}
	store, err := o.store()
	if err != nil {
		return app.Session{}, err
	}
	return app.Configure(store.Workspace, o.config)
}
func emit[T any](cmd *cobra.Command, schema string, value T) error {
	data, err := contracts.Encode(schema, value)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}
func newRootCmd() *cobra.Command { return newCommandTree(nil) }
func newCommandTree(open func(context.Context, string, string, bool) (app.Session, error)) *cobra.Command {
	o := &cliOptions{open: open}
	showVersion := false
	root := &cobra.Command{Use: "nosey", Short: "Discover and install agent skills for your workspace", Args: cobra.NoArgs, SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().StringVar(&o.cwd, "cwd", "", "workspace root (defaults to current directory; never ascends)")
	root.PersistentFlags().StringVarP(&o.config, "config", "c", "", "user JSON settings file")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print version")
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if showVersion {
			cmd.Println(resolvedVersion())
			return nil
		}
		return runNoseyUI(cmd, o)
	}
	root.AddCommand(&cobra.Command{Use: "ui", Short: "Open the four-tab workspace interface", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return runNoseyUI(cmd, o) }})
	root.AddCommand(scanCommand(o), searchCommand(o), installCommand(o), registryCommand(o), settingsCommand(o), trustCommand(o), listCommand(o), doctorCommand(o), newCompletionCmd(), newSkillsCmd(root))
	return root
}
func filterFlags(cmd *cobra.Command, f *domain.Filters, scan bool) {
	flags := cmd.Flags()
	flags.StringVar(&f.Query, "query", "", "case-insensitive query")
	flags.StringSliceVar(&f.Technologies, "technology", nil, "technology IDs")
	flags.StringSliceVar(&f.Registries, "registry", nil, "source registry IDs (result filter)")
	flags.StringSliceVar(&f.Repositories, "repository", nil, "source repository IDs")
	flags.StringSliceVar(&f.Statuses, "status", nil, "any, installed, missing or unknown")
	flags.StringSliceVar(&f.Qualities, "quality", nil, "excellent, good, fair, poor or unassessed")
	flags.StringVar(&f.ScoreState, "score-state", "any", "any, scored or unscored")
	flags.Float64("min-score", 0, "minimum authored score (0..100)")
	flags.Float64("min-repo-average", 0, "minimum authored repository average (0..100)")
	if scan {
		flags.StringSliceVar(&f.RecommendationTechnologies, "rec-technology", nil, "recommendation technology IDs")
		flags.StringSliceVar(&f.RuleKinds, "rule-kind", nil, "matching evidence rule kinds")
		flags.StringSliceVar(&f.Priorities, "priority", nil, "core, recommended or optional")
		flags.StringVar(&f.QueryTarget, "query-target", "both", "technologies, skills or both")
	}
}
func normalizeFilters(cmd *cobra.Command, f *domain.Filters, c domain.Catalog) error {
	for _, facet := range []*[]string{&f.Technologies, &f.RecommendationTechnologies, &f.RuleKinds, &f.Registries, &f.Repositories, &f.Priorities, &f.Statuses, &f.Qualities} {
		if *facet == nil {
			*facet = []string{}
		}
	}
	if f.QueryTarget == "" {
		f.QueryTarget = "both"
	}
	if !slices.Contains([]string{"both", "technologies", "skills"}, f.QueryTarget) {
		return fmt.Errorf("invalid query-target")
	}
	if !slices.Contains([]string{"any", "scored", "unscored"}, f.ScoreState) {
		return fmt.Errorf("invalid score-state")
	}
	for _, status := range f.Statuses {
		if !slices.Contains([]string{"any", "installed", "missing", "unknown"}, status) {
			return fmt.Errorf("invalid status %s", status)
		}
	}
	if slices.Contains(f.Statuses, "any") {
		f.Statuses = []string{}
	}
	for _, id := range f.Registries {
		if _, ok := c.Roots[id]; !ok {
			return fmt.Errorf("unknown registry filter %s", id)
		}
	}
	for _, item := range []struct {
		name   string
		target **float64
	}{{"min-score", &f.MinScore}, {"min-repo-average", &f.MinRepositoryAverage}} {
		if cmd.Flags().Changed(item.name) {
			v, _ := cmd.Flags().GetFloat64(item.name)
			if v < 0 || v > 100 {
				return fmt.Errorf("%s must be 0..100", item.name)
			}
			*item.target = &v
		}
	}
	return nil
}
func scanCommand(o *cliOptions) *cobra.Command {
	f := domain.DefaultFilters()
	sort := domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}
	jsonMode, strict, mcp, refresh := false, false, false, false
	cmd := &cobra.Command{Use: "scan", Short: "Detect technologies and recommend skills", Args: cobra.NoArgs}
	filterFlags(cmd, &f, true)
	cmd.Flags().StringVar(&sort.Technologies, "sort-technologies", "name-asc", "name-asc, name-desc, evidence-count-desc, matched-rules-desc")
	cmd.Flags().StringVar(&sort.Skills, "sort-skills", "priority-desc", "priority-desc, score-desc, quality-desc, repository-average-desc, name-asc")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit scan JSON v2")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 4 when required evaluation is incomplete")
	cmd.Flags().BoolVar(&mcp, "enable-mcp", false, "passively inspect project MCP package references")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "refresh registry snapshots")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		s, err := o.session(cmd.Context(), refresh)
		if err != nil {
			return err
		}
		if err = normalizeFilters(cmd, &f, s.Catalog); err != nil {
			return err
		}
		if err = catalog.ValidateFilters(s.Catalog, f); err != nil {
			return err
		}
		if err = catalog.ValidateSort(sort.Technologies, true, false); err != nil {
			return err
		}
		if err = catalog.ValidateSort(sort.Skills, false, false); err != nil {
			return err
		}
		_ = s.Inventory(cmd.Context())
		result, _, err := s.Scan(cmd.Context(), f, sort, mcp)
		if err != nil {
			return fail(4, err)
		}
		if jsonMode {
			err = emit(cmd, "scan-output.schema.json", result)
		} else {
			for _, t := range result.Technologies {
				cmd.Printf("%s (%d evidence items)\n", t.ID, len(t.Evidence))
			}
			for _, skill := range result.Skills {
				cmd.Printf("%s  %s  %s\n", skill.ID, skill.Priority, strings.Join(skill.Reasons, "; "))
			}
		}
		if err != nil {
			return err
		}
		if !result.Complete {
			cmd.PrintErrln("Warning: required rule evaluation is incomplete; inspect coverage gaps.")
			if strict {
				return fail(4, fmt.Errorf("strict scan incomplete"))
			}
		}
		return nil
	}
	return cmd
}
func searchCommand(o *cliOptions) *cobra.Command {
	f := domain.DefaultFilters()
	order := "relevance"
	jsonMode := false
	cmd := &cobra.Command{Use: "search [QUERY]", Short: "Browse the effective skill catalog", Args: cobra.MaximumNArgs(1)}
	filterFlags(cmd, &f, false)
	cmd.Flags().StringVar(&order, "sort", "relevance", "relevance, score-desc, quality-desc, repository-average-desc, name-asc")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit search JSON v2")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		if len(args) > 0 {
			f.Query = args[0]
		}
		if err = normalizeFilters(cmd, &f, s.Catalog); err != nil {
			return err
		}
		if err = catalog.ValidateFilters(s.Catalog, f); err != nil {
			return err
		}
		if err = catalog.ValidateSort(order, false, true); err != nil {
			return err
		}
		_ = s.Inventory(cmd.Context())
		result := catalog.Search(s.Catalog, f, order)
		if jsonMode {
			return emit(cmd, "search-output.schema.json", result)
		}
		for _, g := range result.Groups {
			cmd.Println(g.Name)
			for _, r := range g.Repositories {
				cmd.Printf("  %s\n", r.Name)
				for _, skill := range r.Skills {
					cmd.Printf("    %s  %s\n", skill.ID, skill.Name)
				}
			}
		}
		return nil
	}
	return cmd
}
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
func listCommand(o *cliOptions) *cobra.Command {
	jsonMode := false
	cmd := &cobra.Command{Use: "list", Short: "Read project-scoped installed skill inventory", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit project inventory JSON v1")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		result := s.Installer.Inventory(cmd.Context(), s.Catalog)
		if jsonMode {
			return emit(cmd, "list-output.schema.json", result)
		}
		for _, item := range result.Skills {
			cmd.Printf("%s  %s\n", item.Name, item.Status)
		}
		return nil
	}
	return cmd
}
func settingsCommand(o *cliOptions) *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "Read or update schema-validated JSON settings"}
	root.AddCommand(&cobra.Command{Use: "get KEY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		data, _ := json.Marshal(resolved.Settings)
		var doc map[string]json.RawMessage
		if err = json.Unmarshal(data, &doc); err != nil {
			return err
		}
		parts := strings.Split(args[0], ".")
		value, ok := doc[parts[0]]
		if len(parts) == 2 {
			var nested map[string]json.RawMessage
			if err = json.Unmarshal(value, &nested); err != nil {
				return err
			}
			value, ok = nested[parts[1]]
		}
		if !ok || len(parts) > 2 {
			return fmt.Errorf("unknown key")
		}
		cmd.Println(string(value))
		return nil
	}})
	project := false
	set := &cobra.Command{Use: "set KEY JSON_VALUE", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		return store.Set(args[0], []byte(args[1]), project)
	}}
	set.Flags().BoolVar(&project, "project", false, "write this workspace's settings layer")
	root.AddCommand(set)
	return root
}
func trustCommand(o *cliOptions) *cobra.Command {
	revoke, yes := false, false
	cmd := &cobra.Command{Use: "trust", Short: "Explicitly approve or revoke workspace registry fingerprints", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "revoke current workspace registry approval")
	cmd.Flags().BoolVar(&yes, "yes", false, "approve the displayed current fingerprint noninteractively")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		if revoke {
			return store.Trust("", true)
		}
		cmd.Printf("Workspace: %s\nFingerprint: %s\n", store.Workspace, resolved.Fingerprint)
		for _, ref := range resolved.Pending {
			cmd.Printf("%s %s%s\n", ref.ID, ref.Source.URI, ref.Source.Path)
		}
		if !yes {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("explicit --yes or interactive approval required")
			}
			yes, err = confirm(cmd, "Approve this workspace registry fingerprint?")
			if err != nil {
				return err
			}
		}
		if !yes {
			return nil
		}
		return store.Trust(resolved.Fingerprint, false)
	}
	return cmd
}

type registryListSource struct {
	Kind     string  `json:"kind"`
	Location string  `json:"location"`
	Ref      *string `json:"requestedRef"`
	Revision *string `json:"resolvedRevision"`
}
type registryListItem struct {
	ID       string             `json:"id"`
	Source   registryListSource `json:"source"`
	Enabled  bool               `json:"enabled"`
	Priority int                `json:"priority"`
	TTL      int                `json:"ttlHours"`
	Trust    string             `json:"trust"`
	Cache    string             `json:"cacheState"`
}
type registryListResult struct {
	Version     int                 `json:"outputVersion"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	Registries  []registryListItem  `json:"registries"`
}

func registryCommand(o *cliOptions) *cobra.Command {
	root := &cobra.Command{Use: "registry", Short: "Manage data-only Git/local registries"}
	jsonMode := false
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		result := registryListResult{Version: 1, Diagnostics: resolved.Diagnostics, Registries: []registryListItem{}}
		for _, ref := range resolved.Settings.Registries {
			location := ref.Source.URI
			if ref.Source.Kind == "local" {
				location = ref.Source.Path
			}
			var requested *string
			if ref.Source.Ref != "" {
				v := ref.Source.Ref
				requested = &v
			}
			state, revision := (registry.Service{Store: store}).Inspect(ref, resolved.Settings.Updates.TTL)
			ttl := resolved.Settings.Updates.TTL
			if ref.TTLHours > 0 {
				ttl = ref.TTLHours
			}
			trust := "not-required"
			if resolved.WorkspaceIDs[ref.ID] {
				trust = "approved"
			}
			result.Registries = append(result.Registries, registryListItem{ref.ID, registryListSource{ref.Source.Kind, location, requested, revision}, ref.Enabled, ref.Priority, ttl, trust, state})
		}
		for _, ref := range resolved.Pending {
			result.Registries = append(result.Registries, registryListItem{ref.ID, registryListSource{ref.Source.Kind, ref.Source.URI + ref.Source.Path, nil, nil}, ref.Enabled, ref.Priority, resolved.Settings.Updates.TTL, "pending", "missing"})
		}
		if jsonMode {
			return emit(cmd, "registry-list-output.schema.json", result)
		}
		for _, ref := range result.Registries {
			cmd.Printf("%s  %s  %s\n", ref.ID, ref.Source.Location, ref.Trust)
		}
		return nil
	}}
	list.Flags().BoolVar(&jsonMode, "json", false, "emit registry inventory JSON v1")
	root.AddCommand(list)
	id := ""
	priority := 20
	add := &cobra.Command{Use: "add URI_OR_PATH", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		refs, err := store.UserRegistries()
		if err != nil {
			return err
		}
		source := domain.RegistrySource{Kind: "git", URI: args[0]}
		if info, statErr := os.Stat(args[0]); statErr == nil && info.IsDir() {
			path, absErr := filepath.Abs(args[0])
			if absErr != nil {
				return absErr
			}
			source = domain.RegistrySource{Kind: "local", Path: path}
		} else if err = registry.ValidateGit(args[0], ""); err != nil {
			return err
		}
		if id == "" {
			id = "user:" + strings.TrimSuffix(filepath.Base(args[0]), ".git")
		}
		if slices.ContainsFunc(refs, func(r domain.RegistryRef) bool { return r.ID == id }) {
			return fmt.Errorf("registry ID already registered")
		}
		refs = append(refs, domain.RegistryRef{ID: id, Source: source, Enabled: true, Priority: priority})
		return store.Registries(refs)
	}}
	add.Flags().StringVar(&id, "id", "", "stable registry ID")
	add.Flags().IntVar(&priority, "priority", 20, "merge priority (0..1000)")
	root.AddCommand(add)
	root.AddCommand(&cobra.Command{Use: "remove ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		refs, err := store.UserRegistries()
		if err != nil {
			return err
		}
		index := slices.IndexFunc(refs, func(r domain.RegistryRef) bool { return r.ID == args[0] })
		if index < 0 {
			return fmt.Errorf("registry is not registered in user settings")
		}
		refs = slices.Delete(refs, index, index+1)
		return store.Registries(refs)
	}})
	all := false
	update := &cobra.Command{Use: "update [ID]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		refs := resolved.Settings.Registries
		if len(args) > 0 {
			if all {
				return fmt.Errorf("ID and --all are exclusive")
			}
			refs = slices.DeleteFunc(refs, func(r domain.RegistryRef) bool { return r.ID != args[0] })
			if len(refs) == 0 {
				return fmt.Errorf("unknown mandatory registry")
			}
		}
		service := registry.Service{Store: store}
		for _, ref := range refs {
			if !ref.Enabled {
				return fmt.Errorf("mandatory registry is disabled")
			}
			snap, err := service.Resolve(cmd.Context(), ref, resolved.Settings.Updates.TTL, true)
			if err != nil {
				return fail(3, err)
			}
			cmd.Printf("%s refreshed (%s)\n", ref.ID, snap.Root)
		}
		return nil
	}}
	update.Flags().BoolVar(&all, "all", false, "refresh all enabled registries")
	root.AddCommand(update)
	standalone := false
	validate := &cobra.Command{Use: "validate PATH", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		path, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		refs := []domain.RegistryRef{}
		if !standalone {
			for _, ref := range resolved.Settings.Registries {
				if ref.ID == "nosey:official" {
					refs = append(refs, ref)
				}
			}
		}
		refs = append(refs, domain.RegistryRef{ID: "validation:input", Source: domain.RegistrySource{Kind: "local", Path: path}, Enabled: true, Priority: 30})
		resolved.Settings.Registries = refs
		c, err := (registry.Service{Store: store}).Load(cmd.Context(), resolved, false)
		if err != nil {
			return err
		}
		for _, d := range c.Diagnostics {
			cmd.PrintErrf("%s: %s\n", d.Code, d.Message)
			if d.Severity == "error" {
				return fmt.Errorf("registry has effective validation errors")
			}
		}
		cmd.Println("Registry is valid")
		return nil
	}}
	validate.Flags().BoolVar(&standalone, "standalone", false, "require references to resolve without the official registry")
	root.AddCommand(validate)
	opts := registry.InitOptions{Template: "workspace", Version: resolvedVersion()}
	yes, jsonInit, fromScan := false, false, false
	initCmd := &cobra.Command{Use: "init PATH", Short: "Create a registry from an external validated template", Args: cobra.ExactArgs(1)}
	initCmd.Flags().StringVar(&opts.Template, "template", "workspace", "empty or workspace")
	initCmd.Flags().StringVar(&opts.RegistryID, "template-registry", "", "configured template provider ID (official by default)")
	initCmd.Flags().BoolVar(&fromScan, "from-scan", false, "seed detected technology definitions/references")
	initCmd.Flags().StringSliceVar(&opts.Include, "include-tech", nil, "explicit technology IDs to seed")
	initCmd.Flags().BoolVar(&opts.Git, "git", false, "initialize Git after successful creation")
	initCmd.Flags().BoolVar(&opts.Register, "register", false, "register the local result in user settings")
	initCmd.Flags().BoolVar(&yes, "yes", false, "confirm registry file creation")
	initCmd.Flags().BoolVar(&jsonInit, "json", false, "emit registry-init JSON v2")
	initCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !yes {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("noninteractive init requires --yes")
			}
			confirmed, err := confirm(cmd, "Create this registry from the selected external template?")
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) && !fromScan && !cmd.Flags().Changed("template") {
			return fmt.Errorf("noninteractive init requires --template or --from-scan")
		}
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		opts.Path = args[0]
		if fromScan && len(opts.Include) == 0 {
			scan, _, err := s.Scan(cmd.Context(), domain.DefaultFilters(), domain.Sort{Technologies: "name-asc", Skills: "priority-desc"}, false)
			if err != nil {
				return err
			}
			for _, t := range scan.Technologies {
				opts.Include = append(opts.Include, t.ID)
			}
		}
		result, err := s.Registry.Init(cmd.Context(), s.Catalog, opts)
		if err != nil {
			return err
		}
		if jsonInit {
			return emit(cmd, "registry-init-output.schema.json", result)
		}
		cmd.Printf("Created %s from %s\n", result.Path, result.SourceRegistryID)
		return nil
	}
	root.AddCommand(initCmd)
	return root
}

type doctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type doctorResult struct {
	Version     int                 `json:"outputVersion"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	Workspace   string              `json:"workspace"`
	Checks      []doctorCheck       `json:"checks"`
}

func doctorCommand(o *cliOptions) *cobra.Command {
	jsonMode := false
	cmd := &cobra.Command{Use: "doctor", Short: "Inspect settings, registry and pinned installer prerequisites", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit doctor JSON v1")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		result := doctorResult{Version: 1, Workspace: store.Workspace, Diagnostics: []domain.Diagnostic{}, Checks: []doctorCheck{}}
		resolved, err := store.Resolve()
		if err != nil {
			result.Checks = append(result.Checks, doctorCheck{"settings", "fail", "E_SETTINGS", storage.Safe(err.Error())})
		} else {
			result.Checks = append(result.Checks, doctorCheck{"settings", "pass", "OK", "JSON settings validated"})
			for _, a := range resolved.Settings.Detection.Adapters {
				result.Checks = append(result.Checks, doctorCheck{"adapter:" + a.ID, "warning", "ADAPTER", a.Version + " sha256=" + a.Hash})
			}
		}
		s, configErr := o.configuredSession(cmd.Context())
		if configErr == nil {
			for _, check := range s.Installer.CheckPrerequisites(cmd.Context()) {
				result.Checks = append(result.Checks, doctorCheck{check.ID, check.Status, check.Code, storage.Safe(check.Message)})
			}
		}
		failed := false
		for _, check := range result.Checks {
			if check.Status == "fail" {
				failed = true
			}
		}
		if jsonMode {
			if err = emit(cmd, "doctor-output.schema.json", result); err != nil {
				return err
			}
		} else {
			for _, c := range result.Checks {
				cmd.Printf("%s %s: %s\n", c.Status, c.ID, c.Message)
			}
		}
		if failed {
			return installation.PrerequisiteError{Reason: "doctor found failed prerequisite checks"}
		}
		return nil
	}
	return cmd
}
func confirm(cmd *cobra.Command, message string) (bool, error) {
	return prompts.Confirm(prompts.ConfirmParams{Context: cmd.Context(), Message: message, InitialValue: false, Input: os.Stdin, Output: os.Stderr})
}
func runNoseyUI(cmd *cobra.Command, o *cliOptions) error {
	if err := requireInteractive(os.Stdin, os.Stdout); err != nil {
		return err
	}
	store, err := o.store()
	if err != nil {
		return err
	}
	return ui.RunNosey(cmd.Context(), store.Workspace, o.config)
}
