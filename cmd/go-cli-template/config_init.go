package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/go-cli-template/internal/adapters/editor"
	"github.com/go-cli-template/internal/domain"
	"github.com/go-cli-template/internal/utils"
)

type configInitOptions struct {
	force        bool
	local        bool
	openInEditor bool
}

func newConfigInitCmd() *cobra.Command {
	opts := &configInitOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a default config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigInit(cmd, opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "overwrite existing config")
	cmd.Flags().BoolVar(&opts.local, "local", false, "write ./.go-cli-template/config.toml instead of the global config")
	cmd.Flags().BoolVarP(&opts.openInEditor, "editor", "e", false, "open config in editor after creation")
	return cmd
}

func runConfigInit(cmd *cobra.Command, opts *configInitOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := utils.ConfigPathGlobal()
	if opts.local {
		path = utils.ConfigPathLocal(cwd)
	}
	if pathExists(path) && !opts.force {
		return fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
	}
	cfg := domain.DefaultConfig()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	content := renderConfigTemplate(cfg)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	if opts.openInEditor {
		editorAdapter := editor.New(cfg.Editor)
		if err := editorAdapter.Open(path); err != nil {
			return err
		}
	}
	cmd.Printf("Wrote config to %s\n", path)
	return nil
}

func renderConfigTemplate(cfg domain.Config) string {
	var builder strings.Builder
	builder.WriteString("# Generic CLI Tool Configuration\n\n")
	builder.WriteString("# General\n")
	fmt.Fprintf(&builder, "# editor = %q\n", cfg.Editor)
	builder.WriteString("\n# UI\n")
	builder.WriteString("# list_spacing options: compact (title only), tight (title + description, no margin), space (default, with spacing)\n")
	fmt.Fprintf(&builder, "# list_spacing = %q\n", cfg.ListSpacing)
	builder.WriteString("\n# Colors\n")
	builder.WriteString("# Colors support named, numeric, or hex values (ex: 7, 13, \"#ff8800\").\n")
	fmt.Fprintf(&builder, "# headings = %q\n", cfg.Headings)
	fmt.Fprintf(&builder, "# primary = %q\n", cfg.Primary)
	fmt.Fprintf(&builder, "# secondary = %q\n", cfg.Secondary)
	fmt.Fprintf(&builder, "# text = %q\n", cfg.Text)
	fmt.Fprintf(&builder, "# text_highlight = %q\n", cfg.TextHighlight)
	fmt.Fprintf(&builder, "# description_highlight = %q\n", cfg.DescriptionHighlight)
	fmt.Fprintf(&builder, "# tags = %q\n", cfg.Tags)
	fmt.Fprintf(&builder, "# flags = %q\n", cfg.Flags)
	fmt.Fprintf(&builder, "# muted = %q\n", cfg.Muted)
	fmt.Fprintf(&builder, "# border = %q\n", cfg.Border)
	return builder.String()
}
