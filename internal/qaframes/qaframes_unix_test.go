//go:build !windows

package qaframes

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func waitDead(t *testing.T, what string, pid int) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if !Alive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("%s (pid %d) is still alive: an orphan", what, pid)
	_ = syscall.Kill(pid, syscall.SIGKILL) // the test cleans up after itself
}

// A run killed mid-way (ctx cancel and a real SIGTERM to the process) leaves no
// daemon, no helper of the daemon, no browser and no temp home.
func TestFramesRun_SelfCleaning_NoOrphans(t *testing.T) {
	for _, mode := range []string{"cancel", "sigterm"} {
		t.Run(mode, func(t *testing.T) {
			tmpRoot := t.TempDir()
			grandFile := filepath.Join(tmpRoot, "grandchild.pid")
			started := make(chan struct{})
			l := &fakeLauncher{
				onDaemon: func(sup *Supervisor, home string) error {
					// a "daemon" that forks a helper in its own group, like smind serve with its agents
					return sup.Start(exec.Command("sh", "-c", "sleep 300 & echo $! > "+grandFile+"; wait"))
				},
				driver: func(ctx context.Context, sup *Supervisor, shots []DriverShot) ([]DriverResult, error) {
					// a "driver" and the "browser" it reports and the supervisor adopts
					br := exec.Command("sleep", "300")
					if err := br.Start(); err != nil {
						return nil, err
					}
					go br.Wait()
					sup.Adopt(br.Process.Pid)
					if err := sup.Start(exec.Command("sleep", "300")); err != nil {
						return nil, err
					}
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				},
			}
			o := baseOpts(t, l, browserEnv(""))
			o.TempRoot = tmpRoot
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "sigterm" {
				var stop context.CancelFunc
				ctx, stop = signal.NotifyContext(context.Background(), syscall.SIGTERM)
				defer stop()
			}
			done := make(chan error, 1)
			go func() { _, err := Run(ctx, o); done <- err }()
			select {
			case <-started:
			case <-time.After(20 * time.Second):
				t.Fatal("the run never reached the driver")
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if b, _ := os.ReadFile(grandFile); strings.TrimSpace(string(b)) != "" {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			grand, _ := strconv.Atoi(strings.TrimSpace(string(readOrEmpty(grandFile))))
			pids := append([]int(nil), l.sup.PIDs()...)
			if len(pids) < 3 || grand == 0 {
				t.Fatalf("test setup: pids %v grandchild %d", pids, grand)
			}
			if mode == "sigterm" {
				_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("Run did not return after the kill")
			}
			for _, p := range pids {
				waitDead(t, "recorded process", p)
			}
			waitDead(t, "the daemon's helper process", grand)
			if _, err := os.Stat(l.home); !os.IsNotExist(err) {
				t.Errorf("temp home %s left behind", l.home)
			}
		})
	}
}

func readOrEmpty(p string) []byte { b, _ := os.ReadFile(p); return b }
