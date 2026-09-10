package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
)

// wireEnvelope mirrors internal/wsapi's wire envelope -- see
// cmd/smind/task.go's doc comment on why the CLI duplicates these wire
// shapes rather than importing wsapi's own (unexported) types: the JSON
// contract is the real interface here, not a shared Go struct.
type wireEnvelope struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Event  string          `json:"event,omitempty"`
}

// TestRunAccountLoginDispatchesOAuthStartAndPrintsAccount drives `smind
// account login <provider> <label>` against a local fake WS server (a raw
// gorilla/websocket handler, not a real wsapi.Handler -- unlike
// account_test.go's account-add test, this one must not exercise a real
// LoginCoordinator, which would actually try to bind the vendor's
// localhost callback port and block for real, waiting on a browser that
// will never arrive). It confirms account.oauthStart is called with the
// right params, the authorize URL from the streamed "authorizeUrl" event
// is printed, and the final account line matches account add's own
// format.
func TestRunAccountLoginDispatchesOAuthStartAndPrintsAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	var gotMethod string
	var gotParams map[string]any
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer ws.Close()

		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage() error = %v", err)
			return
		}
		var req wireEnvelope
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		gotMethod = req.Method
		if err := json.Unmarshal(req.Params, &gotParams); err != nil {
			t.Errorf("unmarshal request params: %v", err)
			return
		}

		eventParams, err := json.Marshal(map[string]string{"url": "https://example.invalid/authorize?state=abc"})
		if err != nil {
			t.Fatalf("marshal event params: %v", err)
		}
		event, err := json.Marshal(wireEnvelope{ID: req.ID, Event: "authorizeUrl", Params: eventParams})
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if err := ws.WriteMessage(websocket.TextMessage, event); err != nil {
			t.Errorf("write event: %v", err)
			return
		}

		result, err := json.Marshal(map[string]any{
			"id": 7, "provider": "anthropic", "label": "work",
			"credentialType": "oauth", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z",
		})
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		term, err := json.Marshal(wireEnvelope{ID: req.ID, Result: result})
		if err != nil {
			t.Fatalf("marshal terminal envelope: %v", err)
		}
		if err := ws.WriteMessage(websocket.TextMessage, term); err != nil {
			t.Errorf("write terminal response: %v", err)
			return
		}

		// Keep the handler alive briefly so the client has a chance to
		// read both messages before this connection tears down.
		_, _, _ = ws.ReadMessage()
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("server:\n  port: "+u.Port()+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	previousStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = write
	defer func() { os.Stdout = previousStdout }()

	code := run([]string{"account", "login", "anthropic", "work"})

	if err := write.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	if _, err := io.Copy(&stdout, read); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := read.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	if code != 0 {
		t.Fatalf("run(account login) = %d, want 0 (stdout: %s)", code, stdout.String())
	}
	if gotMethod != "account.oauthStart" {
		t.Fatalf("RPC method = %q, want account.oauthStart", gotMethod)
	}
	if gotParams["provider"] != "anthropic" || gotParams["label"] != "work" {
		t.Fatalf("RPC params = %+v, want provider=anthropic label=work", gotParams)
	}

	out := stdout.String()
	if !bytes.Contains([]byte(out), []byte("https://example.invalid/authorize?state=abc")) {
		t.Errorf("stdout = %q, want it to contain the authorize URL", out)
	}
	if !bytes.Contains([]byte(out), []byte("7\tanthropic\twork\toauth\n")) {
		t.Errorf("stdout = %q, want the account line in account-add's format", out)
	}
}

func TestRunAccountLoginUsage(t *testing.T) {
	if code := run([]string{"account", "login"}); code != 2 {
		t.Fatalf("run(account login) with no args = %d, want 2", code)
	}
	if code := run([]string{"account", "login", "anthropic"}); code != 2 {
		t.Fatalf("run(account login) with one arg = %d, want 2", code)
	}
}
