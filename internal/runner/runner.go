// Package runner executes definition commands with `sh -c`.
//
// Each command runs in its own process group so that cancelling it (selection
// changed, timeout, quit) kills the whole pipeline, not just the shell.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrTimeout is returned when a command exceeds Request.Timeout.
var ErrTimeout = errors.New("command timed out")

// Request describes one command execution.
type Request struct {
	Cmd     string
	Env     map[string]string // added on top of the inherited environment
	Dir     string
	Timeout time.Duration // 0 = none
}

// Result holds a finished command's output.
type Result struct {
	Stdout []byte
	Stderr []byte
}

// ExitError reports a command that exited non-zero.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return fmt.Sprintf("exit status %d: %s", e.Code, msg)
}

// Runner runs commands. The TUI and engine depend on this so tests can fake it.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
	// Stream runs req until it exits or ctx is cancelled, passing stdout and
	// stderr to onData as they arrive. Request.Timeout is ignored.
	Stream(ctx context.Context, req Request, onData func([]byte)) error
}

// Shell runs commands with /bin/sh.
type Shell struct{}

// Run executes req and waits for it. It returns *ExitError on a non-zero exit,
// ErrTimeout on timeout and ctx.Err() on cancellation.
func (Shell) Run(ctx context.Context, req Request) (Result, error) {
	runCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	cmd := command(runCtx, req)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil && runCtx.Err() != nil && ctx.Err() == nil {
		return res, ErrTimeout
	}
	return res, exitErr(ctx, err, stderr.String())
}

// Stream implements Runner.
func (Shell) Stream(ctx context.Context, req Request, onData func([]byte)) error {
	cmd := command(ctx, req)
	w := &chunkWriter{onData: onData}
	cmd.Stdout = w
	cmd.Stderr = w
	return exitErr(ctx, cmd.Run(), "")
}

// command builds an `sh -c` command in its own process group, so cancelling
// ctx kills the whole pipeline.
func command(ctx context.Context, req Request) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", req.Cmd)
	cmd.Dir = req.Dir
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// Don't hang on grandchildren that somehow keep the output pipes open.
	cmd.WaitDelay = time.Second
	return cmd
}

// exitErr maps a finished command's error to ctx.Err() or *ExitError.
func exitErr(ctx context.Context, err error, stderr string) error {
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return &ExitError{Code: ee.ExitCode(), Stderr: stderr}
	}
	return err
}

// chunkWriter forwards copies of written chunks; stdout and stderr share one,
// so writes are serialised.
type chunkWriter struct {
	mu     sync.Mutex
	onData func([]byte)
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onData(bytes.Clone(p))
	return len(p), nil
}
