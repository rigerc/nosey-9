// Command catalog-import converts the verified skill catalog into an official registry.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/rigerc/nosey-9/internal/catalogimport"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "catalog-import:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("catalog-import", flag.ContinueOnError)
	flags.SetOutput(out)
	inputs := flags.String("inputs", "registry-import/inputs", "directory containing the pinned ledger, catalog and mappings")
	tech := flags.String("technologies", "docs/prd/source-planning-package/reference/technology-registry-v2/technologies-894-v2.json", "unchanged technology reference JSON")
	crosswalk := flags.String("technology-map", "registry-import/technology-id-map.json", "explicit upstream-to-registry technology ID map")
	revision := flags.String("revision", "d1bc68a826cd233f85f291eca0cfa9b4303713e6", "recorded source catalog Git revision")
	destination := flags.String("out", "registries/default", "new registry directory to create (never overwrites an existing path)")
	check := flags.Bool("check", false, "check generated files against an existing registry without writing")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	var in catalogimport.Inputs
	in.Revision = *revision
	for _, file := range []struct {
		path string
		data *[]byte
	}{
		{filepath.Join(*inputs, "catalog-source-ledger.json"), &in.Ledger},
		{filepath.Join(*inputs, "catalog.json"), &in.Catalog},
		{filepath.Join(*inputs, "mappings.json"), &in.Mappings},
		{*tech, &in.Technologies}, {*crosswalk, &in.TechnologyMap},
	} {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", file.path, err)
		}
		*file.data = data
	}
	result, err := catalogimport.Compile(in)
	if err != nil {
		return err
	}
	if *check {
		err = checkFiles(*destination, result.Files)
	} else {
		err = writeNew(*destination, result.Files)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%d skills, %d repositories, %d technologies, %d mappings (%d combinations); frontend bonus ignored\n", result.Skills, result.Repositories, result.Technologies, result.Mappings, result.Combinations)
	return err
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func checkFiles(destination string, files map[string][]byte) error {
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("registry destination must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, name := range sortedNames(files) {
		data, readErr := root.ReadFile(name)
		if readErr != nil {
			return fmt.Errorf("check %s: %w", name, readErr)
		}
		if !bytes.Equal(data, files[name]) {
			return fmt.Errorf("generated file differs: %s", name)
		}
	}
	return nil
}

func writeNew(destination string, files map[string][]byte) error {
	target, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(target); err == nil {
		return fmt.Errorf("destination exists: %s; use --check or generate into a new directory", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(target)
	if err = os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".catalog-import-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for _, name := range sortedNames(files) {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("invalid generated path %q", name)
		}
		dst := filepath.Join(stage, name)
		if err = os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		if err = os.WriteFile(dst, files[name], 0o600); err != nil {
			return err
		}
	}
	if _, err = os.Lstat(target); err == nil {
		return fmt.Errorf("destination appeared while generating: %s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(stage, target)
}
