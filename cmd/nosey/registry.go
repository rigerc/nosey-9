package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

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

func registryListCommand(o *cliOptions) *cobra.Command {
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
	return list
}

func registryAddCommand(o *cliOptions) *cobra.Command {
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
	return add
}

func registryRemoveCommand(o *cliOptions) *cobra.Command {
	return &cobra.Command{Use: "remove ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
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
	}}
}

func registryUpdateCommand(o *cliOptions) *cobra.Command {
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
	return update
}

func registryValidateCommand(o *cliOptions) *cobra.Command {
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
	return validate
}

func registryInitCommand(o *cliOptions) *cobra.Command {
	opts := registry.InitOptions{Version: resolvedVersion()}
	yes, jsonInit := false, false
	ci := ""
	initCmd := &cobra.Command{
		Use:   "init PATH",
		Short: "Create an empty registry from the published template",
		Long: "Create an empty, regenerable registry from a template provider's published template.\n\n" +
			"--ci github adds a workflow that validates pull requests, regenerates on\n" +
			"main and weekly opens one review pull request for upstream data and skill\n" +
			"pins, plus Dependabot for its actions. Update it later with\n" +
			"`nosey registry ci update`.\n\n" +
			"With --github the registry ID is github:OWNER:NAME; otherwise it is\n" +
			"local:DIRECTORY. With --ci github, --github also lets GitHub Actions open\n" +
			"pull requests in the new repository; GitHub ties that setting to approving them.",
		Example: `  nosey registry init ./my-registry --yes
  nosey registry init ./my-registry --ci github --register --yes
  nosey registry init ./my-registry --ci github --github owner/my-registry --yes`,
		Args: cobra.ExactArgs(1),
	}
	initCmd.Flags().StringVar(&opts.RegistryID, "template-registry", "", "configured template provider ID (official by default)")
	initCmd.Flags().StringVar(&ci, "ci", "", "include CI workflow and Dependabot files (github)")
	initCmd.Flags().BoolVar(&opts.Git, "git", false, "initialize Git after successful creation")
	initCmd.Flags().StringVar(&opts.GitHubRepo, "github", "", "create and push a GitHub repository OWNER/NAME with the gh CLI (private unless --public)")
	initCmd.Flags().BoolVar(&opts.Public, "public", false, "make the --github repository public")
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
		if ci != "" && ci != "github" {
			return fmt.Errorf("unsupported CI provider %q (supported: github)", ci)
		}
		opts.CI = ci == "github"
		if opts.GitHubRepo != "" && !registry.ValidGitHubRepo(opts.GitHubRepo) {
			return fmt.Errorf("invalid --github value %q (expected OWNER/NAME)", opts.GitHubRepo)
		}
		if opts.Public && opts.GitHubRepo == "" {
			return fmt.Errorf("--public requires --github")
		}
		s, err := o.session(cmd.Context(), false)
		if err != nil {
			return err
		}
		opts.Path = args[0]
		result, err := s.Registry.Init(cmd.Context(), s.Catalog, opts)
		if err != nil {
			return err
		}
		if jsonInit {
			return emit(cmd, "registry-init-output.schema.json", result)
		}
		cmd.Printf("Created %s from %s\n", result.Path, result.SourceRegistryID)
		for _, d := range result.Diagnostics {
			cmd.PrintErrf("%s: %s\n", d.Code, d.Message)
		}
		if opts.GitHubRepo != "" {
			cmd.Printf("Published https://github.com/%s\n", opts.GitHubRepo)
		}
		return nil
	}
	return initCmd
}

func registryCommand(o *cliOptions) *cobra.Command {
	root := &cobra.Command{Use: "registry", Short: "Manage data-only Git/local registries"}
	root.AddCommand(registryListCommand(o), registryAddCommand(o), registryRemoveCommand(o), registryUpdateCommand(o), registryValidateCommand(o), registryInitCommand(o))
	root.AddCommand(generateCommand(), refreshCommand(), studioCommand(o), registryPinsCommand(), registryCICommand(o))
	return root
}
