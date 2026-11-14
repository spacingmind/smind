package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/crash"
)

// crashDir points the process-default crash recorder at a temp dir for one test.
func crashDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "crashes")
	crash.Init(crash.Config{Dir: dir, Source: "daemon", Version: "test", MaxPerMinute: 1000})
	t.Cleanup(func() { crash.Init(crash.Config{}) })
	return dir
}

func crashReports(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "crash-*.json"))
	return files
}

// crash-ws-dispatch-panic-recorded-error-response and
// crash-ws-connection-survives-panic: a panicking method is answered with a
// generic internal error, leaves a redacted report, and the same connection
// still serves the next request.
func TestCrashWSDispatchPanicRecordedErrorResponse(t *testing.T) {
	dir := crashDir(t)
	hs := map[string]handlerFunc{
		"boom": func(context.Context, *requestContext, json.RawMessage) (any, error) {
			panic("SECRET-PROMPT-TEXT in a panic value")
		},
		"ping": func(context.Context, *requestContext, json.RawMessage) (any, error) { return "pong", nil },
	}
	client, server := newPipeTransportPair()
	c := newConn(server, hs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.serve(ctx)
	defer client.Close()

	call := func(id, method string) envelope {
		data, _ := json.Marshal(envelope{ID: id, Method: method})
		if err := client.Send(data); err != nil {
			t.Fatal(err)
		}
		raw, err := client.Receive()
		if err != nil {
			t.Fatalf("receive after %s: %v", method, err)
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		return env
	}

	resp := call("1", "boom")
	if resp.Error == nil || resp.Error.Message != "internal error" {
		t.Fatalf("boom response = %+v, want a generic internal error", resp)
	}
	if strings.Contains(string(crashJSON(t, resp)), "SECRET-PROMPT-TEXT") {
		t.Fatal("the panic value leaked to the caller")
	}
	files := crashReports(t, dir)
	if len(files) != 1 {
		t.Fatalf("reports = %d, want 1", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if strings.Contains(string(data), "SECRET-PROMPT-TEXT") || !strings.Contains(string(data), `"site":"wsapi.dispatch"`) {
		t.Fatalf("report leaked the message or lost its site: %s", data)
	}
	// the connection lives
	if pong := call("2", "ping"); pong.Error != nil || !strings.Contains(string(pong.Result), "pong") {
		t.Fatalf("ping after panic = %+v", pong)
	}
}

func crashJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
