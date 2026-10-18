package wsapi

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

type oauthTestEnv struct {
	srv      *httptest.Server
	registry *accounts.Registry
	login    *fakeOAuthLogin
}

func newOAuthTestEnv(t *testing.T) oauthTestEnv {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	login := &fakeOAuthLogin{addr: freeAddr(t), path: "/callback"}
	coord := accounts.NewLoginCoordinator(registry, map[string]accounts.Login{"fake": login})
	return oauthTestEnv{
		srv:      newTestWSServerWithCoordinator(t, wm, registry, coord, s, "tok"),
		registry: registry,
		login:    login,
	}
}

// startOAuth sends account.oauthStart and returns the loginId and state of the
// authorizeUrl event it streams.
func startOAuth(t *testing.T, ws *websocket.Conn, reqID string) (loginID, state, authorizeURL string) {
	t.Helper()
	sendRequest(t, ws, reqID, "account.oauthStart", map[string]any{"provider": "fake", "label": "pasted"})
	event := readEnvelopeFor(t, ws, reqID, 5*time.Second)
	if event.Event != "authorizeUrl" {
		t.Fatalf("first message = %+v, want an authorizeUrl event", event)
	}
	var params struct {
		LoginID string `json:"loginId"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(event.Params, &params); err != nil {
		t.Fatalf("decode authorizeUrl params: %v", err)
	}
	u, err := url.Parse(params.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", params.URL, err)
	}
	return params.LoginID, u.Query().Get("state"), params.URL
}

func TestServer_AccountOAuthComplete(t *testing.T) {
	t.Parallel()

	t.Run("oauthStart emits authorizeUrl with a loginId", func(t *testing.T) {
		t.Parallel()
		env := newOAuthTestEnv(t)
		ws := dialWS(t, env.srv, "tok")

		loginID, state, authorizeURL := startOAuth(t, ws, "1")
		if len(loginID) != 22 {
			t.Errorf("loginId = %q, want 22 base64url characters", loginID)
		}
		if state == "" || !strings.HasPrefix(authorizeURL, "https://example.invalid/authorize?") {
			t.Errorf("authorizeUrl = %q, want the vendor's authorize URL carrying a state", authorizeURL)
		}
		// url stays a first-class field next to loginId (the CLI reads it).
		sendRequest(t, ws, "2", "account.oauthComplete", map[string]any{"loginId": loginID, "redirectUrl": "http://localhost:54545/callback?code=c&state=" + state})
		if resp := readEnvelopeFor(t, ws, "2", 5*time.Second); resp.Error != nil {
			t.Fatalf("account.oauthComplete error = %v", resp.Error.Message)
		}
		readEnvelopeFor(t, ws, "1", 5*time.Second)
	})

	t.Run("oauthComplete finishes the login from a pasted redirect", func(t *testing.T) {
		t.Parallel()
		env := newOAuthTestEnv(t)
		// The login is driven from one connection and completed from another,
		// as a web client pasting into a daemon it reaches remotely would.
		ws := dialWS(t, env.srv, "tok")
		other := dialWS(t, env.srv, "tok")

		loginID, state, _ := startOAuth(t, ws, "1")
		sendRequest(t, other, "c1", "account.oauthComplete", map[string]any{
			"loginId":     loginID,
			"redirectUrl": "http://localhost:54545/callback?code=pasted-code&state=" + url.QueryEscape(state),
		})
		done := readEnvelopeFor(t, other, "c1", 5*time.Second)
		if done.Error != nil {
			t.Fatalf("account.oauthComplete error = %v", done.Error.Message)
		}
		if string(done.Result) != "{}" {
			t.Errorf("account.oauthComplete result = %s, want {}", done.Result)
		}

		term := readEnvelopeFor(t, ws, "1", 5*time.Second)
		if term.Error != nil {
			t.Fatalf("account.oauthStart error = %v", term.Error.Message)
		}
		if !bytes.Contains(term.Result, []byte(`"created":true`)) || !bytes.Contains(term.Result, []byte(`"label":"pasted"`)) {
			t.Fatalf("account.oauthStart result = %s, want created:true on the pasted account", term.Result)
		}
		var created accountResult
		mustDecode(t, term.Result, &created)
		stored, err := env.registry.Get(created.ID)
		if err != nil || stored.OAuth == nil || stored.OAuth.RefreshToken != "refresh-for-pasted-code" {
			t.Fatalf("stored account = %+v, %v, want the pasted code's credential", stored, err)
		}
	})

	t.Run("oauthComplete errors carry a code and never echo the paste", func(t *testing.T) {
		t.Parallel()
		env := newOAuthTestEnv(t)
		ws := dialWS(t, env.srv, "tok")

		call := func(id string, params any) envelope {
			sendRequest(t, ws, id, "account.oauthComplete", params)
			return readEnvelopeFor(t, ws, id, 5*time.Second)
		}
		wantErr := func(resp envelope, prefix string) {
			t.Helper()
			if resp.Error == nil || !strings.HasPrefix(resp.Error.Message, prefix) {
				t.Fatalf("response = %+v, want an error starting %q", resp, prefix)
			}
		}

		wantErr(call("1", map[string]any{"loginId": "nope", "redirectUrl": "code=c&state=s"}), "account.oauthComplete: login_unknown: ")
		wantErr(call("2", map[string]any{"loginId": "", "redirectUrl": "code=c"}), "account.oauthComplete: loginId and redirectUrl are required")
		bad := call("3", map[string]any{"loginId": 5, "redirectUrl": "SECRET-PASTE"})
		wantErr(bad, "account.oauthComplete: invalid params")
		if strings.Contains(bad.Error.Message, "SECRET-PASTE") {
			t.Errorf("error %q echoes the redirect", bad.Error.Message)
		}

		loginID, state, _ := startOAuth(t, ws, "s")
		wantErr(call("4", map[string]any{"loginId": loginID, "redirectUrl": "not a redirect"}), "account.oauthComplete: login_bad_redirect: ")
		wantErr(call("5", map[string]any{"loginId": loginID, "redirectUrl": "code=c&state=wrong"}), "account.oauthComplete: state_mismatch: ")
		// Still pending: the right paste finishes it.
		if resp := call("6", map[string]any{"loginId": loginID, "redirectUrl": "c#" + state}); resp.Error != nil {
			t.Fatalf("account.oauthComplete error = %v", resp.Error.Message)
		}
		readEnvelopeFor(t, ws, "s", 5*time.Second)
	})
}

// TestServer_AccountOAuthComplete_NoSecretsLeak is "login secrets never
// appear in logs or errors" at the wire level: the pasted URL, code and state
// never show up in any error the RPCs return or in anything logged while they
// run. It is not parallel because it swaps the process-wide log and slog
// destinations.
func TestServer_AccountOAuthComplete_NoSecretsLeak(t *testing.T) {
	var logs bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})

	env := newOAuthTestEnv(t)
	ws := dialWS(t, env.srv, "tok")
	loginID, state, _ := startOAuth(t, ws, "s")

	const (
		secretCode  = "SECRETCODE-9f8e7d"
		secretWrong = "WRONGSTATE-1a2b3c"
		secretJunk  = "JUNKPASTE-4d5e6f"
	)
	pastedWrong := "http://localhost:54545/callback?code=" + secretCode + "&state=" + secretWrong
	pastedJunk := "https://example.com/" + secretJunk
	pastedUnknown := "http://localhost:54545/callback?code=" + secretCode + "&state=" + state
	pastedRight := "http://localhost:54545/callback?code=" + secretCode + "&state=" + state

	var messages []string
	call := func(id, loginID, paste string) {
		sendRequest(t, ws, id, "account.oauthComplete", map[string]any{"loginId": loginID, "redirectUrl": paste})
		if resp := readEnvelopeFor(t, ws, id, 5*time.Second); resp.Error != nil {
			messages = append(messages, resp.Error.Message)
		}
	}
	call("1", loginID, pastedWrong)
	call("2", loginID, pastedJunk)
	call("3", "unknown-login-id", pastedUnknown)
	call("4", loginID, pastedRight)
	term := readEnvelopeFor(t, ws, "s", 5*time.Second)
	if term.Error != nil {
		t.Fatalf("account.oauthStart error = %v", term.Error.Message)
	}
	if len(messages) != 3 {
		t.Fatalf("collected %d error messages %q, want the 3 rejected pastes", len(messages), messages)
	}

	// The wrong paste, junk paste and unknown id are rejected; the last paste
	// succeeded, so the start request has no error.
	for _, msg := range messages[:3] {
		if !strings.Contains(msg, "account.oauthComplete: ") {
			t.Errorf("error %q, want the method prefix", msg)
		}
	}
	for _, secret := range []string{secretCode, secretWrong, secretJunk, state, pastedWrong, pastedJunk, pastedRight} {
		for _, msg := range messages {
			if strings.Contains(msg, secret) {
				t.Errorf("an RPC error leaks %q: %q", secret, msg)
			}
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs leak %q: %q", secret, logs.String())
		}
		if bytes.Contains(term.Result, []byte(secret)) {
			t.Errorf("account.oauthStart result leaks %q: %s", secret, term.Result)
		}
	}
}
