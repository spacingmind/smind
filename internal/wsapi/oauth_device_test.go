package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

const (
	testDeviceAuthID = "DEVAUTH-secret-id-5c1f"
	testDeviceCode   = "WSAPI-4321"
)

// fakeDeviceOAuthLogin is a fakeOAuthLogin that also offers the device flow:
// it hands out a code, then approves the first poll when approve is set (and
// stays pending otherwise).
type fakeDeviceOAuthLogin struct {
	*fakeOAuthLogin
	approve bool
}

func (f *fakeDeviceOAuthLogin) StartDevice(context.Context) (accounts.DeviceChallenge, error) {
	return accounts.DeviceChallenge{
		DeviceAuthID:    testDeviceAuthID,
		UserCode:        testDeviceCode,
		VerificationURL: "https://example.invalid/device",
		Interval:        time.Second,
	}, nil
}

func (f *fakeDeviceOAuthLogin) PollDevice(context.Context, accounts.DeviceChallenge) (accounts.DeviceGrant, error) {
	if f.approve {
		return accounts.DeviceGrant{AuthorizationCode: "dev-code", CodeVerifier: "dev-verifier"}, nil
	}
	return accounts.DeviceGrant{}, accounts.ErrDevicePending
}

func (f *fakeDeviceOAuthLogin) ExchangeDevice(_ context.Context, g accounts.DeviceGrant) (accounts.ExchangeResult, error) {
	return f.Exchange(context.Background(), g.AuthorizationCode, "", accounts.PKCECodes{})
}

// newDeviceTestEnv serves provider "fake" (device capable, per login) and
// "plain" (browser only).
func newDeviceTestEnv(t *testing.T, login *fakeDeviceOAuthLogin) oauthTestEnv {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	registry := accounts.New(s)
	plain := &fakeOAuthLogin{addr: freeAddr(t), path: "/callback"}
	coord := accounts.NewLoginCoordinator(registry, map[string]accounts.Login{"fake": login, "plain": plain})
	return oauthTestEnv{
		srv:      newTestWSServerWithCoordinator(t, workspace.New(s), registry, coord, s, "tok"),
		registry: registry,
		login:    login.fakeOAuthLogin,
	}
}

func readRaw(t *testing.T, ws *websocket.Conn, timeout time.Duration) []byte {
	t.Helper()
	if err := ws.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	return data
}

func TestServer_AccountOAuthDevice(t *testing.T) {
	t.Parallel()

	t.Run("device login emits deviceCode to requester only", func(t *testing.T) {
		t.Parallel()
		env := newDeviceTestEnv(t, &fakeDeviceOAuthLogin{fakeOAuthLogin: &fakeOAuthLogin{addr: freeAddr(t), path: "/callback"}})
		requester := dialWS(t, env.srv, "tok")
		bystander := dialWS(t, env.srv, "tok")

		sendRequest(t, requester, "1", "account.oauthStart", map[string]any{"provider": "fake", "label": "dev", "method": "device"})
		raw := readRaw(t, requester, 5*time.Second)

		var msg struct {
			ID     string          `json:"id"`
			Event  string          `json:"event"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode event %s: %v", raw, err)
		}
		if msg.ID != "1" || msg.Event != "deviceCode" {
			t.Fatalf("first message = %s, want a deviceCode event for request 1", raw)
		}
		var params map[string]any
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			t.Fatalf("decode params %s: %v", msg.Params, err)
		}
		if len(params) != 5 {
			t.Errorf("params = %s, want exactly loginId, verificationUrl, userCode, expiresAt, intervalSeconds", msg.Params)
		}
		if id, _ := params["loginId"].(string); len(id) != 22 {
			t.Errorf("loginId = %v, want 22 base64url characters", params["loginId"])
		}
		if params["verificationUrl"] != "https://example.invalid/device" || params["userCode"] != testDeviceCode {
			t.Errorf("params = %s, want the verification URL and user code", msg.Params)
		}
		if params["intervalSeconds"] != float64(1) {
			t.Errorf("intervalSeconds = %v, want 1", params["intervalSeconds"])
		}
		expires, err := time.Parse(time.RFC3339, params["expiresAt"].(string))
		if err != nil {
			t.Fatalf("expiresAt %v is not RFC3339: %v", params["expiresAt"], err)
		}
		if until := time.Until(expires); until < 14*time.Minute || until > 16*time.Minute {
			t.Errorf("expiresAt in %v, want about 15 minutes", until)
		}
		// The vendor's device id never leaves the daemon.
		if strings.Contains(string(raw), testDeviceAuthID) || strings.Contains(strings.ToLower(string(raw)), "device_auth") || strings.Contains(string(raw), "deviceAuth") {
			t.Errorf("event %s carries the vendor's device_auth_id", raw)
		}

		// Nobody else hears about it.
		if err := bystander.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline() error = %v", err)
		}
		_, other, err := bystander.ReadMessage()
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("bystander read = %s, %v, want a timeout (no message)", other, err)
		}

		// Cancelling the request ends the login.
		sendCancel(t, requester, "1")
		if term := readEnvelopeFor(t, requester, "1", 5*time.Second); term.Error == nil {
			t.Errorf("terminal message = %+v, want the cancelled login's error", term)
		}
	})

	t.Run("device login finishes through account.oauthStart", func(t *testing.T) {
		t.Parallel()
		env := newDeviceTestEnv(t, &fakeDeviceOAuthLogin{fakeOAuthLogin: &fakeOAuthLogin{addr: freeAddr(t), path: "/callback", subject: "sub"}, approve: true})
		ws := dialWS(t, env.srv, "tok")

		sendRequest(t, ws, "1", "account.oauthStart", map[string]any{"provider": "fake", "label": "dev", "method": "device"})
		term := readEnvelopeFor(t, ws, "1", 10*time.Second)
		if term.Event != "deviceCode" {
			t.Fatalf("first message = %+v, want the deviceCode event", term)
		}
		term = readEnvelopeFor(t, ws, "1", 10*time.Second)
		if term.Error != nil {
			t.Fatalf("account.oauthStart error = %v", term.Error.Message)
		}
		var res struct {
			Created     bool `json:"created"`
			HasIdentity bool `json:"hasIdentity"`
		}
		if err := json.Unmarshal(term.Result, &res); err != nil || !res.Created || !res.HasIdentity {
			t.Fatalf("account.oauthStart result = %s, want created:true with an identity", term.Result)
		}
		if strings.Contains(string(term.Result), testDeviceAuthID) || strings.Contains(string(term.Result), "dev-verifier") {
			t.Errorf("result %s carries a device secret", term.Result)
		}
	})

	t.Run("device method errors carry stable codes", func(t *testing.T) {
		t.Parallel()
		env := newDeviceTestEnv(t, &fakeDeviceOAuthLogin{fakeOAuthLogin: &fakeOAuthLogin{addr: freeAddr(t), path: "/callback"}})
		ws := dialWS(t, env.srv, "tok")

		for id, tc := range map[string]struct {
			provider, method, wantPrefix string
		}{
			"1": {"plain", "device", "account.oauthStart: device_unsupported: "},
			"2": {"fake", "carrier-pigeon", "account.oauthStart: login_bad_method: "},
		} {
			sendRequest(t, ws, id, "account.oauthStart", map[string]any{"provider": tc.provider, "label": "x", "method": tc.method})
			resp := readEnvelopeFor(t, ws, id, 5*time.Second)
			if resp.Error == nil || !strings.HasPrefix(resp.Error.Message, tc.wantPrefix) {
				t.Errorf("%s/%s error = %+v, want prefix %q", tc.provider, tc.method, resp.Error, tc.wantPrefix)
			} else if strings.Contains(resp.Error.Message, "carrier-pigeon") {
				t.Errorf("error %q echoes the method", resp.Error.Message)
			}
		}
	})
}
