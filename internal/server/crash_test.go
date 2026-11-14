package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/crash"
)

func crashTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "crashes")
	crash.Init(crash.Config{Dir: dir, Source: "daemon", Version: "test", MaxPerMinute: 1000})
	t.Cleanup(func() { crash.Init(crash.Config{}) })
	return dir
}

// crash-http-handler-panic-recorded: a handler panic leaves one redacted
// report, ends that connection, and the server keeps answering.
func TestCrashHTTPHandlerPanicRecorded(t *testing.T) {
	dir := crashTestDir(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/boom", func(http.ResponseWriter, *http.Request) { panic("SECRET-BODY-TEXT") })
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("fine")) })
	ts := httptest.NewServer(recoverPanics(mux))
	defer ts.Close()

	if resp, err := http.Get(ts.URL + "/boom"); err == nil {
		resp.Body.Close()
		t.Fatal("the panicking request should have its connection aborted")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "crash-*.json"))
	if len(files) != 1 {
		t.Fatalf("reports = %d, want 1", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if strings.Contains(string(data), "SECRET-BODY-TEXT") || !strings.Contains(string(data), `"site":"http"`) {
		t.Fatalf("report leaked the message or lost its site: %s", data)
	}
	resp, err := http.Get(ts.URL + "/ok")
	if err != nil {
		t.Fatalf("server did not survive the panic: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// crash-http-abort-sentinel-not-recorded: http.ErrAbortHandler is a deliberate
// abort, not a crash.
func TestCrashHTTPAbortHandlerNotRecorded(t *testing.T) {
	dir := crashTestDir(t)
	ts := httptest.NewServer(recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })))
	defer ts.Close()
	if resp, err := http.Get(ts.URL); err == nil {
		resp.Body.Close()
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "crash-*.json")); len(files) != 0 {
		t.Fatalf("reports = %d, want 0 for ErrAbortHandler", len(files))
	}
}

// crash-http-handler-wired: Server.Handler() is wrapped by recoverPanics (a
// source check, since no real route can be made to panic).
func TestCrashHTTPHandlerWired(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "return recoverPanics(s.direct.guard(mux))") {
		t.Fatal("Server.Handler() must return recoverPanics(...) as its outermost handler")
	}
}
