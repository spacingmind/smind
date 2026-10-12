package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

// fakeUsageDial returns a dial func whose fake `account.usage` handler
// records the params it was called with and replies with result.
func fakeUsageDial(t *testing.T, result *accountUsageResponse, callErr error) (dial func(context.Context) (wsCaller, error), gotParams *map[string]any) {
	t.Helper()
	gotParams = &map[string]any{}
	return func(ctx context.Context) (wsCaller, error) {
		return func(_ context.Context, method string, params any, out any) error {
			*gotParams = params.(map[string]any)
			if callErr != nil {
				return callErr
			}
			b, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal fake result: %v", err)
			}
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("unmarshal fake result: %v", err)
			}
			return nil
		}, nil
	}, gotParams
}

func TestAccountUsageDefaultIsTouchNotRefresh(t *testing.T) {
	dial, got := fakeUsageDial(t, &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{}}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, time.Now(), &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	if m := (*got)["mode"]; m != "touch" {
		t.Fatalf("params mode = %v, want \"touch\"", m)
	}
}

func TestAccountUsageRefreshFlag(t *testing.T) {
	dial, got := fakeUsageDial(t, &accountUsageResponse{Enabled: true, Reports: []accountUsageEntry{}}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, true, false, time.Now(), &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if m := (*got)["mode"]; m != "refresh" {
		t.Fatalf("params mode = %v, want \"refresh\"", m)
	}
}

func TestAccountUsageJSON(t *testing.T) {
	fetched := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	dial, _ := fakeUsageDial(t, &accountUsageResponse{
		Enabled: true, TTLSeconds: 300,
		Reports: []accountUsageEntry{{
			AccountID: 1, Key: "anthropic-1", Provider: "anthropic", DisplayName: "Work",
			CredentialType: "oauth", Report: &quota.Report{Version: 2, Status: quota.StatusAvailable, FetchedAt: fetched},
		}},
	}, nil)
	var out bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, true, time.Now(), &out, &out); code != 0 {
		t.Fatalf("code = %d, want 0; out: %s", code, out.String())
	}
	var back accountUsageResponse
	if err := json.Unmarshal(out.Bytes(), &back); err != nil {
		t.Fatalf("stdout is not the daemon JSON: %v\n%s", err, out.String())
	}
	if !back.Enabled || len(back.Reports) != 1 || back.Reports[0].Key != "anthropic-1" || back.Reports[0].Report == nil {
		t.Fatalf("json = %s, want enabled response with one report", out.String())
	}
	for _, secret := range []string{"credentialData", "credential_data", "fingerprint", "apiKey", "api_key"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("json leaks %q: %s", secret, out.String())
		}
	}
}

func TestAccountUsageDaemonDownExitCode(t *testing.T) {
	dial := func(ctx context.Context) (wsCaller, error) {
		return nil, errors.New("connect to smind daemon at 127.0.0.1:4648 (is `smind serve` running?): refused")
	}
	var stdout, stderr bytes.Buffer
	if code := runAccountUsage(context.Background(), dial, false, false, time.Now(), &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "smind serve") {
		t.Fatalf("stderr = %q, want the daemon-down hint", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
}

func TestAccountUsageUnknownFlagExits2(t *testing.T) {
	previous := osStderr
	osStderr = &bytes.Buffer{}
	defer func() { osStderr = previous }()
	if code := cmdAccountUsage([]string{"--bogus"}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}
