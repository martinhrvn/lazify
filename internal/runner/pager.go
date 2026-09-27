package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/martinhrvn/lazify/internal/tmpl"
)

func quote(s string) string { return tmpl.Quote(s) }

// PagerCommand is the shell command that pipes producer into pager through
// lazify's pager helper (`lazify __pager`, see RunPager). self is lazify's
// own executable.
func PagerCommand(self, producer, pager string) string {
	return "exec " + quote(self) + " __pager " + quote(producer) + " " + quote(pager)
}

// RunPager runs producer with its output (stdout and stderr) piped into
// pager, which has the terminal otherwise. When the pager exits the producer
// is killed with its whole process group: in a plain `producer | pager` the
// shell would wait for the producer, which may not notice the closed pipe
// for a long time (a quiet `tail -f`). Returns the pager's exit code.
func RunPager(producer, pager string, stdout, stderr io.Writer) int {
	// Ctrl+C belongs to the pager (less stops following); the helper only
	// notices it. Handled, not ignored, so children get the default. A hangup
	// (the floating pane was closed) or TERM ends everything, the producer's
	// group included: it is not in the terminal's group, so it isn't hung up.
	sig := make(chan os.Signal, 4)
	signal.Notify(sig, os.Interrupt, syscall.SIGQUIT, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(sig)
	var (
		mu       sync.Mutex // guards prod, pg against the signal goroutine
		prod, pg *exec.Cmd
	)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case s := <-sig:
				if s == syscall.SIGHUP || s == syscall.SIGTERM {
					mu.Lock()
					if prod != nil && prod.Process != nil {
						syscall.Kill(-prod.Process.Pid, syscall.SIGTERM)
					}
					if pg != nil && pg.Process != nil {
						pg.Process.Signal(syscall.SIGTERM)
					}
					mu.Unlock()
				}
			}
		}
	}()

	r, w, err := os.Pipe()
	if err != nil {
		io.WriteString(stderr, "lazify: "+err.Error()+"\n")
		return 1
	}
	mu.Lock()
	prod = exec.Command("/bin/sh", "-c", producer)
	prod.Stdout, prod.Stderr = w, w
	prod.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // killed as a group
	err = prod.Start()
	mu.Unlock()
	if err != nil {
		io.WriteString(stderr, "lazify: "+err.Error()+"\n")
		return 1
	}
	w.Close() // the producer has its own copy

	mu.Lock()
	pg = exec.Command("/bin/sh", "-c", pager)
	pg.Stdin, pg.Stdout, pg.Stderr = r, stdout, stderr
	err = pg.Start()
	mu.Unlock()
	if err == nil {
		err = pg.Wait()
	}
	r.Close()
	syscall.Kill(-prod.Process.Pid, syscall.SIGTERM)
	prod.Wait()

	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	io.WriteString(stderr, "lazify: "+err.Error()+"\n")
	return 1
}

// Float returns a request that runs req in a floating pane of the terminal
// multiplexer lazify is in — a tmux popup or a zellij floating pane — and
// whether there is one. The multiplexer starts the pane, not lazify, so req's
// env, PATH and dir are set explicitly in the pane's script.
func Float(req Request, title string) (Request, bool) {
	var script strings.Builder
	keys := make([]string, 0, len(req.Env))
	for k := range req.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		script.WriteString("export " + k + "=" + quote(req.Env[k]) + "; ")
	}
	script.WriteString("export PATH=" + quote(os.Getenv("PATH")) + "; ")
	if req.Dir != "" {
		script.WriteString("cd " + quote(req.Dir) + "; ")
	}
	script.WriteString(req.Cmd)

	out := Request{Env: req.Env, Dir: req.Dir}
	switch {
	case os.Getenv("TMUX") != "": // innermost first: tmux may run inside zellij
		out.Cmd = "tmux display-popup -E -w 90% -h 90% -T " + quote(" "+title+" ") + " " +
			quote("sh -c "+quote(script.String()))
	case os.Getenv("ZELLIJ") != "":
		out.Cmd = "zellij run --floating --close-on-exit --width 90% --height 90% -x 5% -y 5% --name " +
			quote(title) + " -- sh -c " + quote(script.String())
	default:
		return Request{}, false
	}
	return out, true
}
