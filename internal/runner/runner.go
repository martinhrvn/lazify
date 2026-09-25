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

	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", req.Cmd)
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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}

	switch {
	case err == nil:
		return res, nil
	case ctx.Err() != nil:
		return res, ctx.Err()
	case runCtx.Err() != nil:
		return res, ErrTimeout
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return res, &ExitError{Code: ee.ExitCode(), Stderr: stderr.String()}
	}
	return res, err
}
