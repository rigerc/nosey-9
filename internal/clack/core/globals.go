package core

import (
	"errors"
	"os"

	"github.com/go-cli-template/internal/clack/reflectutil"
)

var (
	// ErrCancelPrompt indicates that the user cancelled the prompt.
	ErrCancelPrompt = errors.New("prompt canceled")
)

// FileSystem is the file system surface used by path prompts.
type FileSystem interface {
	Getwd() (string, error)
	ReadDir(name string) ([]os.DirEntry, error)
	UserHomeDir() (string, error)
}

// WrapRender wraps a render function for a specific prompt type (TPrompt) into a function compatible with the Prompt[T] type.
// It allows custom rendering logic to be applied to a prompt.
//
// Parameters:
//   - p (TPrompt): The prompt instance to be passed to the render function.
//   - render (func(p TPrompt) string): The custom render function that generates the prompt's frame.
//
// Returns:
//   - func(_ *Prompt[T]) string: A function that can be used as the render function for a Prompt[T].
func WrapRender[T any, TPrompt any](p TPrompt, render func(p TPrompt) string) func(_ *Prompt[T]) string {
	return func(_ *Prompt[T]) string {
		return render(p)
	}
}

// WrapValidate wraps a validation function and combines it with a required flag and custom error message.
// It ensures that the value is validated against the provided rules and returns an error if validation fails.
//
// Parameters:
//   - validate (func(value TValue) error): The custom validation function to apply to the value.
//   - isRequired (*bool): A pointer to a boolean indicating whether the value is required.
//   - errMsg (string): The error message to return if the value is required but not provided or invalid.
//
// Returns:
//   - func(value TValue) error: A function that validates the value and returns an error if validation fails.
func WrapValidate[TValue any](validate func(value TValue) error, isRequired *bool, errMsg string) func(value TValue) error {
	return func(value TValue) error {
		if validate == nil && !*isRequired {
			return nil
		}

		if validate != nil {
			if err := validate(value); err != nil {
				return err
			}
		}

		if *isRequired && reflectutil.RequiredMissing(value) {
			return errors.New(errMsg)
		}

		return nil
	}
}
