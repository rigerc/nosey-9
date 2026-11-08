// Package registrygen canonicalizes registry data and produces the published
// manifest and lock file from a single in-repo data set.
package registrygen

import (
	"errors"
	"fmt"
	"strings"
)

// DriftError reports generated files that differ from disk during --check.
// The listed paths are relative to the registry root.
type DriftError struct{ Paths []string }

func (e *DriftError) Error() string {
	if len(e.Paths) == 0 {
		return "registry output drift detected"
	}
	return "registry output drift detected: " + strings.Join(e.Paths, ", ")
}

// ExitCode implements the CLI exit-code contract for drift.
func (e *DriftError) ExitCode() int { return 2 }

// InputError reports invalid configuration, documents, or references.
type InputError struct{ Err error }

func (e *InputError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying validation error.
func (e *InputError) Unwrap() error { return e.Err }

// ExitCode implements the CLI exit-code contract for invalid input.
func (e *InputError) ExitCode() int { return 2 }

// RefreshError reports an upstream resolution or fetch failure.
type RefreshError struct{ Err error }

func (e *RefreshError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying fetch error.
func (e *RefreshError) Unwrap() error { return e.Err }

// ExitCode implements the CLI exit-code contract for source failures.
func (e *RefreshError) ExitCode() int { return 3 }

func inputError(format string, args ...any) error {
	return &InputError{Err: fmt.Errorf(format, args...)}
}

func refreshError(format string, args ...any) error {
	return &RefreshError{Err: fmt.Errorf(format, args...)}
}

// Coder is implemented by errors that carry an explicit process exit code.
type Coder interface {
	error
	ExitCode() int
}

// Code returns the exit code carried by err, or 3 for unclassified I/O errors.
func Code(err error) int {
	if err == nil {
		return 0
	}
	var coded Coder
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 3
}
