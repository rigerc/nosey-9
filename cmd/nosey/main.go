// Package main implements the nosey command.
package main

import (
	"github.com/rigerc/nosey-9/internal/storage"
	"os"
)

func main() {
	if err := Execute(); err != nil {
		_, _ = os.Stderr.WriteString(storage.Safe(err.Error()) + "\n")
		os.Exit(exitCode(err))
	}
}
