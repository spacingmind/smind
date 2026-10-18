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
	"strings"
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

	// Stub the opener: the real one launches the developer's browser.
	var opened string
	previousOpenBrowser := openBrowser
	openBrowser = func(u string) { opened = u }
	defer func() { openBrowser = previousOpenBrowser }()

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
	if opened != "https://example.invalid/authorize?state=abc" {
		t.Errorf("openBrowser got %q, want the authorize URL", opened)
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

func TestParseAccountLoginArgs(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args     []string
		provider string
		label    string
		device   bool
		ok       bool
	}{
		"plain":            {[]string{"openai", "work"}, "openai", "work", false, true},
		"device first":     {[]string{"--device", "openai", "work"}, "openai", "work", true, true},
		"device between":   {[]string{"openai", "--device", "work"}, "openai", "work", true, true},
		"device last":      {[]string{"openai", "work", "--device"}, "openai", "work", true, true},
		"no args":          {nil, "", "", false, false},
		"one positional":   {[]string{"--device", "openai"}, "", "", false, false},
		"three positional": {[]string{"a", "b", "c"}, "", "", false, false},
		"unknown flag":     {[]string{"--browser", "openai", "work"}, "", "", false, false},
		"short flag":       {[]string{"-d", "openai", "work"}, "", "", false, false},
	} {
		provider, label, device, ok := parseAccountLoginArgs(tc.args)
		if provider != tc.provider || label != tc.label || device != tc.device || ok != tc.ok {
			t.Errorf("%s: parseAccountLoginArgs(%q) = %q, %q, %v, %v; want %q, %q, %v, %v",
				name, tc.args, provider, label, device, ok, tc.provider, tc.label, tc.device, tc.ok)
		}
	}
}

// TestRunAccountLoginDeviceFlag: `account login --device` sends method "device"
// in account.oauthStart's params, prints the streamed device code without
// opening a browser, and a plain login sends no method.
func TestRunAccountLoginDeviceFlag(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		wantMethod any
	}{
		{"device", []string{"account", "login", "--device", "openai", "work"}, "device"},
		{"browser", []string{"account", "login", "openai", "work"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SMIND_HOME", home)

			gotParams := make(chan map[string]any, 1)
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
				var params map[string]any
				if err := json.Unmarshal(req.Params, &params); err != nil {
					t.Errorf("unmarshal params: %v", err)
				}
				gotParams <- params

				for _, env := range []wireEnvelope{
					{ID: req.ID, Event: "deviceCode", Params: json.RawMessage(`{"loginId":"l","verificationUrl":"https://example.invalid/device","userCode":"ABCD-1234","expiresAt":"2030-01-01T00:00:00Z","intervalSeconds":5}`)},
					{ID: req.ID, Result: json.RawMessage(`{"id":7,"provider":"openai","label":"work","credentialType":"oauth","created":true}`)},
				} {
					out, _ := json.Marshal(env)
					if err := ws.WriteMessage(websocket.TextMessage, out); err != nil {
						t.Errorf("write: %v", err)
						return
					}
				}
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
			var opened []string
			previousOpenBrowser := openBrowser
			openBrowser = func(u string) { opened = append(opened, u) }
			defer func() { openBrowser = previousOpenBrowser }()

			code := run(tc.args)

			_ = write.Close()
			_, _ = io.Copy(&stdout, read)
			_ = read.Close()

			if code != 0 {
				t.Fatalf("run(%q) = %d, want 0 (stdout: %s)", tc.args, code, stdout.String())
			}
			params := <-gotParams
			if params["provider"] != "openai" || params["label"] != "work" || params["method"] != tc.wantMethod {
				t.Errorf("params = %+v, want provider=openai label=work method=%v", params, tc.wantMethod)
			}
			if !strings.Contains(stdout.String(), "ABCD-1234") || !strings.Contains(stdout.String(), "https://example.invalid/device") {
				t.Errorf("stdout = %q, want the device code and URL", stdout.String())
			}
			if len(opened) != 0 {
				t.Errorf("browser opened for %q, want none", opened)
			}
		})
	}
}

// TestPrintLoginEventDeviceCode: a deviceCode event prints the verification URL
// and the user code and never opens a browser; an authorizeUrl event still
// does; malformed and unknown events print nothing.
func TestPrintLoginEventDeviceCode(t *testing.T) {
	var opened []string
	previousOpenBrowser := openBrowser
	openBrowser = func(u string) { opened = append(opened, u) }
	defer func() { openBrowser = previousOpenBrowser }()

	var out bytes.Buffer
	printLoginEvent(&out, "deviceCode", json.RawMessage(`{"loginId":"l","verificationUrl":"https://example.invalid/device","userCode":"ABCD-1234","expiresAt":"2030-01-01T00:00:00Z","intervalSeconds":5}`))
	want := "Open https://example.invalid/device and enter this code to log in:\nABCD-1234\n"
	if out.String() != want {
		t.Errorf("deviceCode output = %q, want %q", out.String(), want)
	}
	if len(opened) != 0 {
		t.Errorf("openBrowser called with %q for a device login, want no browser", opened)
	}

	out.Reset()
	printLoginEvent(&out, "deviceCode", json.RawMessage(`{"verificationUrl":"https://example.invalid/device"}`))
	printLoginEvent(&out, "deviceCode", json.RawMessage(`not json`))
	printLoginEvent(&out, "somethingElse", json.RawMessage(`{}`))
	if out.Len() != 0 {
		t.Errorf("malformed or unknown events printed %q, want nothing", out.String())
	}

	printLoginEvent(&out, "authorizeUrl", json.RawMessage(`{"loginId":"l","url":"https://example.invalid/a","pasteOnly":true}`))
	if len(opened) != 1 || opened[0] != "https://example.invalid/a" || !strings.Contains(out.String(), "https://example.invalid/a") {
		t.Errorf("authorizeUrl: opened %q, output %q, want the URL printed and opened", opened, out.String())
	}
}

// TestPrintLoginResult: a login that replaced an existing same-identity
// account says so (the requested label was ignored); a new account prints the
// plain account line, as does a daemon that sends no `created` field at all.
func TestPrintLoginResult(t *testing.T) {
	t.Parallel()

	no, yes := false, true
	for name, tc := range map[string]struct {
		created *bool
		want    string
	}{
		"updated": {&no, "updated work (already connected)\n"},
		"created": {&yes, "7\tanthropic\twork\toauth\n"},
		"absent":  {nil, "7\tanthropic\twork\toauth\n"},
	} {
		var out bytes.Buffer
		printLoginResult(&out, loginResult{
			accountResult: accountResult{ID: 7, Provider: "anthropic", Label: "work", CredentialType: "oauth"},
			Created:       tc.created,
		})
		if out.String() != tc.want {
			t.Errorf("%s: printLoginResult = %q, want %q", name, out.String(), tc.want)
		}
	}
}
