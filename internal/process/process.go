// Package process runs bounded, cancellable subprocesses without a shell.
package process

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Executor is the injectable subprocess boundary used by registry and installer services.
type Executor interface {
	Run(context.Context, string, []string, string, ...string) (Output, error)
}

// Output holds separately bounded stdout and stderr.
type Output struct{ Stdout, Stderr []byte }

// Runner configures subprocess time and output budgets.
type Runner struct {
	Timeout time.Duration
	Limit   int
}
type limitBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(p[:min(n, remaining)])
	}
	if n > remaining {
		b.overflow = true
	}
	return n, nil
}

// Run executes explicit argument tokens with cancellation and bounded output.
func (r Runner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (Output, error) {
	return r.run(ctx, dir, env, nil, name, args...)
}

// RunInput supplies bounded JSON stdin to an explicitly trusted detector.
func (r Runner) RunInput(ctx context.Context, dir string, env []string, input []byte, name string, args ...string) (Output, error) {
	return r.run(ctx, dir, env, input, name, args...)
}

// Command builds an interactive command. Cancellation interrupts it so it can clean up.
func Command(ctx context.Context, dir string, env []string, name string, args ...string) *exec.Cmd {
	// #nosec G204 -- callers validate untrusted tokens; execution never invokes a shell.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}
func (r Runner) run(ctx context.Context, dir string, env []string, input []byte, name string, args ...string) (Output, error) {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 45 * time.Second
	}
	limit := r.Limit
	if limit == 0 {
		limit = 4 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- callers validate untrusted tokens; execution never invokes a shell.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = time.Second
	out := &limitBuffer{limit: limit}
	errout := &limitBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = errout
	err := cmd.Run()
	result := Output{out.buffer.Bytes(), errout.buffer.Bytes()}
	if out.overflow || errout.overflow {
		return result, fmt.Errorf("subprocess output exceeds limit")
	}
	return result, err
}
