package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

// stubPush points pushDial at a fake caller that returns result (or err) and
// records the params it saw.
func stubPush(t *testing.T, result any, callErr error) *any {
	t.Helper()
	var seen any
	old := pushDial
	pushDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, params any, out any) error {
			if method != "push.test" {
				t.Errorf("method = %q", method)
			}
			seen = params
			if callErr != nil {
				return callErr
			}
			raw, _ := json.Marshal(result)
			return json.Unmarshal(raw, out)
		}, func() {}, nil
	}
	t.Cleanup(func() { pushDial = old })
	return &seen
}

func runPushCmd(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = runPush(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func results(rs ...map[string]any) map[string]any { return map[string]any{"results": rs} }

func TestPushTestPrintsResultsAndExitsZero(t *testing.T) {
	stubPush(t, results(
		map[string]any{"deviceId": "dev-aaaa", "sent": true},
		map[string]any{"deviceId": "dev-bbbb", "sent": true},
	), nil)
	code, out, errOut := runPushCmd("test")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	for _, want := range []string{"DEVICE", "RESULT", "dev-aaaa", "dev-bbbb", "sent", "2 of 2 sent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestPushTestPartialFailureExitsOne(t *testing.T) {
	stubPush(t, results(
		map[string]any{"deviceId": "dev-aaaa", "sent": true},
		map[string]any{"deviceId": "dev-bbbb", "sent": false, "error": "DeviceNotRegistered"},
	), nil)
	code, out, _ := runPushCmd("test")
	if code != 1 || !strings.Contains(out, "DeviceNotRegistered") || !strings.Contains(out, "1 of 2 sent") {
		t.Fatalf("code %d stdout:\n%s", code, out)
	}
}

func TestPushTestPassesThePrefix(t *testing.T) {
	seen := stubPush(t, results(map[string]any{"deviceId": "abcd1111", "sent": true}), nil)
	if code, _, _ := runPushCmd("test", "abcd"); code != 0 {
		t.Fatalf("code %d", code)
	}
	if m, ok := (*seen).(map[string]any); !ok || m["deviceId"] != "abcd" || len(m) != 1 {
		t.Fatalf("params = %#v", *seen)
	}
	runPushCmd("test")
	if m, ok := (*seen).(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("params without prefix = %#v", *seen)
	}
}

func TestPushTestNoRegistrations(t *testing.T) {
	stubPush(t, results(), nil)
	code, out, errOut := runPushCmd("test")
	if code != 1 || out != "" || !strings.Contains(errOut, "No phone is registered for push. In the app: Connection -> Notifications -> Turn on.") {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestPushTestNoDaemon(t *testing.T) {
	old := pushDial
	pushDial = func(context.Context) (wsCaller, func(), error) {
		return nil, nil, fmt.Errorf("connect: %w", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	}
	t.Cleanup(func() { pushDial = old })
	code, _, errOut := runPushCmd("test")
	if code != 1 || !strings.Contains(errOut, "smind: no daemon is running (start it with `smind serve`)") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestPushTestOtherDialError(t *testing.T) {
	old := pushDial
	pushDial = func(context.Context) (wsCaller, func(), error) { return nil, nil, fmt.Errorf("bad token") }
	t.Cleanup(func() { pushDial = old })
	code, _, errOut := runPushCmd("test")
	if code != 1 || !strings.Contains(errOut, "bad token") || strings.Contains(errOut, "no daemon") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestPushTestErrorMapping(t *testing.T) {
	cases := []struct{ code, msg, want string }{
		{"push_disabled", "x", "push notifications are disabled on this daemon (push.enabled / SMIND_PUSH)"},
		{"not_found", "x", "no phone with that device id is registered for push"},
		{"ambiguous", "x", "that device id prefix matches several devices; use more characters"},
		{"invalid_params", "deviceId prefix must be at least 4 characters", "deviceId prefix must be at least 4 characters"},
		{"other", "boom", "boom"},
	}
	for _, tc := range cases {
		stubPush(t, nil, &wsclient.RPCError{Code: tc.code, Message: tc.msg})
		code, out, errOut := runPushCmd("test", "abcd")
		if code != 1 || out != "" || strings.TrimSpace(errOut) != "smind: "+tc.want {
			t.Errorf("%s: code %d stdout %q stderr %q", tc.code, code, out, errOut)
		}
	}
}

func TestPushUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"test", "a", "b"}} {
		code, out, errOut := runPushCmd(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "smind push test [<device id prefix>]") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, out, errOut)
		}
	}
}

func TestPushTestNeverPrintsTokens(t *testing.T) {
	stubPush(t, results(map[string]any{"deviceId": "dev-aaaa", "sent": true}), nil)
	_, out, errOut := runPushCmd("test")
	if regexp.MustCompile(`ExponentPushToken|ExpoPushToken`).MatchString(out + errOut) {
		t.Fatalf("token-like text in output:\n%s%s", out, errOut)
	}
}
