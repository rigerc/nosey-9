//go:build !unix

package skillkit

import "errors"

func osMkfifo(string) error { return errors.New("mkfifo unsupported") }
