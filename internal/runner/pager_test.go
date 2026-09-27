package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Quitting the pager stops the command feeding it, even one that never ends
// or writes nothing for a while (aws logs tail --follow).
func TestRunPagerStopsProducer(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	producer := `echo $$ > ` + pidFile + `; while :; do echo tick; sleep 0.05; done`
	var out bytes.Buffer
	done := make(chan int)
	go func() { done <- RunPager(producer, "head -n 3", &out, &out) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunPager didn't return after the pager quit")
	}
	if got := strings.Count(out.String(), "tick"); got != 3 {
		t.Errorf("pager output %q", out.String())
	}
	b, _ := os.ReadFile(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("pid file: %q", b)
	}
	for i := 0; i < 50 && syscall.Kill(-pid, 0) == nil; i++ {
		time.Sleep(20 * time.Millisecond) // signals are async; zombies wait to be reaped
	}
	if syscall.Kill(-pid, 0) == nil {
		t.Errorf("producer group %d still alive", pid)
	}
}

func TestRunPagerSilentProducer(t *testing.T) {
	// A producer that writes nothing: the pager exits on its own, the
	// producer must not keep RunPager waiting.
	done := make(chan int)
	go func() { done <- RunPager("sleep 60", "true", &bytes.Buffer{}, &bytes.Buffer{}) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a silent producer kept the pager helper waiting")
	}
}

func TestPagerCommand(t *testing.T) {
	got := PagerCommand("/bin/lazify", `aws logs tail --since '1h'`, "less -R +F")
	want := `exec /bin/lazify __pager 'aws logs tail --since '\''1h'\''' 'less -R +F'`
	if got != want {
		t.Errorf("PagerCommand = %s\nwant           %s", got, want)
	}
}

func TestFloat(t *testing.T) {
	req := Request{Cmd: "tail -f log", Dir: "/srv", Env: map[string]string{"AWS_PROFILE": "prod x"}}
	t.Setenv("PATH", "/bin")
	t.Setenv("TMUX", "")
	t.Setenv("ZELLIJ", "")
	if _, ok := Float(req, "Follow logs"); ok {
		t.Error("no multiplexer: no float")
	}
	script := `export AWS_PROFILE='prod x'; export PATH=/bin; cd /srv; tail -f log`
	t.Setenv("ZELLIJ", "0")
	fr, ok := Float(req, "Follow logs")
	if want := "zellij run --floating --close-on-exit --width 90% --height 90% -x 5% -y 5% --name 'Follow logs' -- sh -c " + quote(script); !ok || fr.Cmd != want {
		t.Errorf("zellij = %s\nwant       %s", fr.Cmd, want)
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0") // tmux inside zellij: tmux is closer
	fr, ok = Float(req, "Follow logs")
	if want := "tmux display-popup -E -w 90% -h 90% -T ' Follow logs ' " + quote("sh -c "+quote(script)); !ok || fr.Cmd != want {
		t.Errorf("tmux = %s\nwant     %s", fr.Cmd, want)
	}
	if fr.Timeout != 0 {
		t.Error("a floating pane has no timeout")
	}
}

// Closing a floating pane hangs up the helper: it must take the producer
// (its own process group, so it doesn't get the hangup) and the pager along.
func TestRunPagerHangup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	done := make(chan int)
	go func() {
		done <- RunPager(`echo $$ > `+pidFile+`; while :; do echo tick; sleep 0.05; done`, "sleep 30", &bytes.Buffer{}, &bytes.Buffer{})
	}()
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		b, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	time.Sleep(100 * time.Millisecond) // let RunPager start the pager
	syscall.Kill(os.Getpid(), syscall.SIGHUP)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hangup should end the helper (and its pager)")
	}
	for i := 0; i < 50 && syscall.Kill(-pid, 0) == nil; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(-pid, 0) == nil {
		t.Errorf("producer group %d survived the hangup", pid)
	}
}
