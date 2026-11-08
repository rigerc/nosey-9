// Package main is an inline gallery of every go-clack prompt form.
//
// Run `go run ./examples/forms all` to walk through every demo, or name a
// single demo from `go run ./examples/forms list`.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/rigerc/nosey-9/internal/clack/prompts"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if prompts.IsCancel(err) {
			_, _ = fmt.Fprintln(os.Stderr, "\nCancelled.")
			return
		}
		_, _ = fmt.Fprintln(os.Stderr, "forms:", err)
		os.Exit(1)
	}
}

// demo is one self-contained prompt demonstration.
type demo struct {
	name    string
	summary string
	run     func() error
}

func allDemos() []demo {
	return []demo{
		{name: "banner", summary: "FIGlet banner", run: runBanner},
		{name: "banner-animated", summary: "Inline animated FIGlet banner", run: runAnimatedBanner},
		{name: "banner-effects", summary: "Wipe, gradient sweep, and fade banner effects", run: runBannerEffects},
		{name: "banner-workflow-sweep", summary: "Gradient sweep above a live prompt", run: runWorkflowSweepBanner},
		{name: "banner-workflow", summary: "Animated banner above an interactive workflow", run: runWorkflowAnimatedBanner},
		{name: "messages", summary: "Intro, message, info, success, warn, error, and outro", run: runMessages},
		{name: "note", summary: "Bordered note box", run: runNote},
		{name: "text", summary: "Text input with placeholder and validation", run: runText},
		{name: "password", summary: "Masked password input", run: runPassword},
		{name: "confirm", summary: "Yes/no confirmation", run: runConfirm},
		{name: "select", summary: "Single select with filtering", run: runSelect},
		{name: "multi-select", summary: "Multi-select with hints", run: runMultiSelect},
		{name: "group-multi-select", summary: "Grouped multi-select", run: runGroupMultiSelect},
		{name: "select-key", summary: "Select by key press", run: runSelectKey},
		{name: "reorder", summary: "Reorder an ordered list", run: runReorder},
		{name: "path", summary: "Path input with completion", run: runPath},
		{name: "select-path", summary: "Browse and select one path", run: runSelectPath},
		{name: "multi-select-path", summary: "Browse and select multiple paths", run: runMultiSelectPath},
		{name: "spinner", summary: "Spinner with a final status", run: runSpinner},
		{name: "tasks", summary: "Sequential background tasks", run: runTasks},
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "all":
		return runAll()
	case "list":
		printList()
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	}

	demos := allDemos()
	for i := range demos {
		if demos[i].name == args[0] {
			return runDemo(demos[i])
		}
	}
	return fmt.Errorf("unknown demo %q; run `forms list` to see the demos", args[0])
}

func runAll() error {
	_, _ = fmt.Fprintln(os.Stdout, "")
	prompts.Intro("Inline go-clack form gallery")
	for _, d := range allDemos() {
		if err := runDemo(d); err != nil {
			return err
		}
	}
	prompts.Outro("Every form rendered inline. Thanks for trying them!")
	return nil
}

func runDemo(d demo) error {
	prompts.Step(d.summary)
	return d.run()
}

func printUsage() {
	_, _ = fmt.Fprintln(os.Stdout, "go-clack inline form gallery")
	_, _ = fmt.Fprintln(os.Stdout, "")
	_, _ = fmt.Fprintln(os.Stdout, "Usage:")
	_, _ = fmt.Fprintln(os.Stdout, "  forms all       Run every form in sequence")
	_, _ = fmt.Fprintln(os.Stdout, "  forms list      List the individual demos")
	_, _ = fmt.Fprintln(os.Stdout, "  forms <demo>    Run one demo, for example `forms text`")
}

func printList() {
	demos := allDemos()
	sort.Slice(demos, func(i, j int) bool { return demos[i].name < demos[j].name })
	for _, d := range demos {
		_, _ = fmt.Fprintf(os.Stdout, "  %-18s %s\n", d.name, d.summary)
	}
}

// show prints a submitted value through the go-clack message formatter.
func show(label, value string) {
	prompts.Message(fmt.Sprintf("%s: %s", label, value), prompts.MessageOptions{})
}
