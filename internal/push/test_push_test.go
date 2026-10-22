package push

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestTestPushSendsOneBatchAndOpens(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	a := h.add("dev-a", 1, nil)
	b := h.add("dev-b", 2, nil)
	res := h.n.Test(context.Background(), []string{"dev-a", "dev-b"})
	if len(res) != 2 || !res[0].Sent || !res[1].Sent || res[0].DeviceID != "dev-a" || res[1].DeviceID != "dev-b" {
		t.Fatalf("results = %+v", res)
	}
	if len(h.sender.calls) != 1 || len(h.sender.calls[0]) != 2 {
		t.Fatalf("calls = %d", len(h.sender.calls))
	}
	for i, r := range []Registration{a, b} {
		ev := h.open(r, h.sender.calls[0][i])
		if ev.Kind != KindTest || ev.Title != "smind" || ev.Body != "Test notification" || ev.TaskID != 0 {
			t.Fatalf("event %d = %+v", i, ev)
		}
	}
}

func TestTestPushBypassesKindsAndRateLimit(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, []string{KindPermission})
	for i := 0; i < 10; i++ {
		res := h.n.Test(context.Background(), []string{"dev-a"})
		if !res[0].Sent {
			t.Fatalf("call %d: %+v", i, res)
		}
	}
	if len(h.sender.calls) != 10 {
		t.Fatalf("sends = %d, want 10", len(h.sender.calls))
	}
}

func TestTestPushNotRegisteredAndNotApproved(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.appr = func(string) bool { return false }
	res := h.n.Test(context.Background(), []string{"nope", "dev-a"})
	if res[0].Error != "not_registered" || res[0].Sent || res[1].Error != "not_approved" || res[1].Sent {
		t.Fatalf("results = %+v", res)
	}
	if len(h.sender.calls) != 0 {
		t.Fatal("sender reached")
	}
}

func TestTestPushProviderErrors(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.add("dev-b", 2, nil)
	h.add("dev-c", 3, nil)
	h.sender.results = []Result{{ErrCode: "DeviceNotRegistered"}, {ErrCode: "InvalidCredentials"}, {OK: true}}
	res := h.n.Test(context.Background(), []string{"dev-a", "dev-b", "dev-c"})
	if res[0].Error != "DeviceNotRegistered" || res[1].Error != "InvalidCredentials" || !res[2].Sent {
		t.Fatalf("results = %+v", res)
	}
	if _, ok := h.reg.Get("dev-a"); ok {
		t.Fatal("dev-a should be removed")
	}
	if _, ok := h.reg.Get("dev-b"); !ok {
		t.Fatal("dev-b should be kept")
	}
}

func TestTestPushSendErrorMarksEveryDevice(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.add("dev-b", 2, nil)
	h.sender.err = errors.New("network down")
	res := h.n.Test(context.Background(), []string{"dev-a", "dev-b"})
	for _, r := range res {
		if r.Sent || r.Error != "send_failed" {
			t.Fatalf("results = %+v", res)
		}
	}
	if len(h.reg.List()) != 2 {
		t.Fatal("registrations changed")
	}
}

func TestTestPushLogsAreSecretFree(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	bad := Registration{
		DeviceID: "bad-device-1", Provider: ProviderExpo, Token: "ExponentPushToken[badtok]",
		Platform: PlatformAndroid, Key: []byte{1, 2, 3}, Kinds: AllKinds, RegisteredAt: h.now,
	}
	if err := h.reg.Put(bad); err != nil {
		t.Fatal(err)
	}
	regs := []Registration{h.add("dev-gone", 1, nil), h.add("dev-rej", 20, nil)}
	h.sender.results = []Result{{ErrCode: "DeviceNotRegistered"}, {ErrCode: "MismatchSenderId"}}
	ids := []string{"bad-device-1", "dev-gone", "dev-rej"}
	h.n.Test(context.Background(), ids)
	h.sender.results = nil
	h.sender.err = errors.New("post failed for ExponentPushToken[dev-rejtok]")
	h.n.Test(context.Background(), []string{"dev-rej"})

	if len(h.logs) == 0 {
		t.Fatal("expected some logs")
	}
	secrets := []string{"Test notification", "badtok"}
	for _, r := range append(regs, bad) {
		secrets = append(secrets, r.Token, hex.EncodeToString(r.Key), base64.StdEncoding.EncodeToString(r.Key))
	}
	for _, line := range h.logs {
		for _, s := range secrets {
			if strings.Contains(line, s) {
				t.Fatalf("log line %q contains %q", line, s)
			}
		}
	}
}
