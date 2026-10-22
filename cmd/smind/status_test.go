package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRenderStatus(t *testing.T) {
	base := statusInfo{
		Addr: "127.0.0.1:4648", Home: "/h/.smind", ConfigPath: "/h/.smind/config.yaml",
		ConfigExists: true, Reachable: true, Service: "smind", Version: "0.14.0",
	}
	tests := []struct {
		name   string
		mutate func(*statusInfo)
		want   []string
		absent []string
	}{
		{"reachable", func(*statusInfo) {}, []string{
			"daemon  http://127.0.0.1:4648\n",
			"home    /h/.smind\n",
			"config  /h/.smind/config.yaml (exists)\n",
			"status  reachable, smind 0.14.0\n",
		}, []string{"managed by"}},
		{"managed", func(i *statusInfo) { i.Version = "dev"; i.ManagedBy = "launchd" },
			[]string{"status  reachable, smind dev (managed by launchd)\n"}, nil},
		{"unreachable", func(i *statusInfo) { i.Reachable = false; i.ProbeErr = os.ErrDeadlineExceeded },
			[]string{"status  NOT reachable: " + os.ErrDeadlineExceeded.Error() + "\n"}, []string{"reachable, smind"}},
		{"wrong service", func(i *statusInfo) { i.Service = "nginx" },
			[]string{"status  something else answers on this port (service=nginx)\n"}, nil},
		{"config absent", func(i *statusInfo) { i.ConfigExists = false },
			[]string{"config  /h/.smind/config.yaml (absent: defaults)\n"}, nil},
		{"config error", func(i *statusInfo) { i.ConfigErr = os.ErrInvalid },
			[]string{"config  ERROR: " + os.ErrInvalid.Error() + "\n"}, []string{"status "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := base
			tt.mutate(&info)
			var sb strings.Builder
			renderStatus(&sb, info)
			for _, w := range tt.want {
				if !strings.Contains(sb.String(), w) {
					t.Errorf("output %q missing %q", sb.String(), w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(sb.String(), a) {
					t.Errorf("output %q should not contain %q", sb.String(), a)
				}
			}
		})
	}
}

// TestStatusReportsDaemon: the wsapi test daemon (newConfigOptionTestEnv)
// requires a token on every route, /healthz included, because it does not
// mount the real internal/server mux. So status is pointed at a stub that
// serves handleHealth's JSON shape; the in-process daemon is still started
// to prove the SMIND_HOME/config plumbing the other commands share.
func TestStatusReportsDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	newConfigOptionTestEnv(t, home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","service":"smind","version":"0.14.0","managedBy":"launchd"}`))
	}))
	defer srv.Close()
	writeTestConfig(t, home, srv.URL)

	code, out := runCaptured(t, []string{"status"})
	if code != 0 {
		t.Fatalf("run(status) = %d, want 0; stdout = %q", code, out)
	}
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	for _, want := range []string{"reachable, smind 0.14.0 (managed by launchd)", "http://127.0.0.1:" + port, home} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q missing %q", out, want)
		}
	}
}

func TestStatusUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	cfg := "server:\n  port: " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runCaptured(t, []string{"status"})
	if code != 1 {
		t.Fatalf("run(status) = %d, want 1; stdout = %q", code, out)
	}
	if !strings.Contains(out, "NOT reachable") {
		t.Errorf("stdout %q missing NOT reachable", out)
	}
}

func TestStatusWrongService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"service":"other"}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	writeTestConfig(t, home, srv.URL)
	code, out := runCaptured(t, []string{"status"})
	if code != 1 || !strings.Contains(out, "something else answers") {
		t.Fatalf("code = %d, stdout = %q", code, out)
	}
}

func TestStatusRejectsArgs(t *testing.T) {
	code := -1
	stderr := captureStderr(t, func() { code = run([]string{"status", "extra"}) })
	if code != 2 || !strings.Contains(stderr, "usage: smind status") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}
