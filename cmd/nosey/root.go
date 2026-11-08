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

	"github.com/bueti/skillgen"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	pkg "github.com/rigerc/nosey-9/internal/package"
)

// Metadata loaded from package.toml at build time
var version = pkg.Version()

var rootCmd = newRootCmd()

// Execute is the CLI entrypoint.
func Execute() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return rootCmd.ExecuteContext(ctx)
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

// requireInteractive reports whether both input and output are attached to a
// terminal. Bubble Tea needs a TTY for key input and cursor control.
func requireInteractive(in, out *os.File) error {
	if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return errors.New("interactive terminal required: run this command from a terminal")
	}
	return nil
}
