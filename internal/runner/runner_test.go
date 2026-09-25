package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCapturesStdout(t *testing.T) {
	res, err := Shell{}.Run(context.Background(), Request{Cmd: "printf 'a\\nb'"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "a\nb" {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestRunEnv(t *testing.T) {
	res, err := Shell{}.Run(context.Background(), Request{
		Cmd: `echo "$LAZIFY_TEST"`,
		Env: map[string]string{"LAZIFY_TEST": "hello world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(res.Stdout)) != "hello world" {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestRunInheritsEnvironment(t *testing.T) {
	t.Setenv("LAZIFY_INHERITED", "yes")
	res, _ := Shell{}.Run(context.Background(), Request{Cmd: `echo "$LAZIFY_INHERITED"`})
	if strings.TrimSpace(string(res.Stdout)) != "yes" {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestRunDir(t *testing.T) {
	dir := t.TempDir()
	res, _ := Shell{}.Run(context.Background(), Request{Cmd: "pwd -P", Dir: dir})
	want, _ := filepath.EvalSymlinks(dir)
	if strings.TrimSpace(string(res.Stdout)) != want {
		t.Errorf("pwd = %q, want %q", res.Stdout, want)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	res, err := Shell{}.Run(context.Background(), Request{Cmd: "echo out; echo boom >&2; exit 3"})
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *ExitError", err)
	}
	if ee.Code != 3 || strings.TrimSpace(ee.Stderr) != "boom" {
		t.Errorf("got code %d stderr %q", ee.Code, ee.Stderr)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("Error() = %q should include stderr", err.Error())
	}
	if strings.TrimSpace(string(res.Stdout)) != "out" {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestRunTimeout(t *testing.T) {
	start := time.Now()
	_, err := Shell{}.Run(context.Background(), Request{Cmd: "sleep 5", Timeout: 100 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v, want ErrTimeout", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}

func TestRunCancelKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// The grandchild would outlive a plain kill of `sh`.
		_, err := Shell{}.Run(ctx, Request{Cmd: "sleep 30 & echo $! > " + pidFile + "; wait"})
		done <- err
	}()

	var pid int
	deadline := time.Now().Add(2 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(b), "\n") {
			pid = atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child never started")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	deadline = time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive after cancel", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
