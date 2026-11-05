package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func stubRetention(t *testing.T, result string, seen *map[string]any) {
	t.Helper()
	prev := retentionDial
	t.Cleanup(func() { retentionDial = prev })
	retentionDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, params any, out any) error {
			if seen != nil {
				*seen = map[string]any{"method": method, "params": params}
			}
			return json.Unmarshal([]byte(result), out)
		}, func() {}, nil
	}
}

func TestRunsPruneCommand(t *testing.T) {
	var seen map[string]any
	stubRetention(t, `{"enabled":true,"days":30,"cutoff":"2026-10-06T00:00:00Z","dryRun":true,"runs":12,"rows":3400,"bytes":52428800}`, &seen)
	var out, errb bytes.Buffer
	if code := runRuns(context.Background(), []string{"prune", "--dry-run"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if seen["method"] != "runs.prune" || seen["params"].(map[string]any)["dryRun"] != true {
		t.Fatalf("call = %v", seen)
	}
	for _, want := range []string{"retention 30 days", "would prune the events of 12 runs: 3400 rows, about 50.0 MiB"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	stubRetention(t, `{"enabled":true,"days":30,"cutoff":"2026-10-06T00:00:00Z","runs":2,"rows":10,"bytes":1048576,"freedBytes":524288,"reusableMB":3.5}`, &seen)
	out.Reset()
	if code := runRuns(context.Background(), []string{"prune"}, &out, &errb); code != 0 || seen["params"].(map[string]any)["dryRun"] != false || !strings.Contains(out.String(), "pruned the events of 2 runs") || !strings.Contains(out.String(), "smind db compact") {
		t.Fatalf("real run (%d): %s", code, out.String())
	}
	stubRetention(t, `{"enabled":false,"days":0}`, nil)
	out.Reset()
	if code := runRuns(context.Background(), []string{"prune"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "retention is off") {
		t.Fatalf("retention off (%d): %s", code, out.String())
	}
}

func TestRunsAndDBUsage(t *testing.T) {
	var out, errb bytes.Buffer
	for _, args := range [][]string{nil, {"prune", "--force"}, {"list"}} {
		if code := runRuns(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("runs %v: exit %d, want 2", args, code)
		}
	}
	for _, args := range [][]string{nil, {"vacuum"}, {"compact", "x"}} {
		if code := runDB(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("db %v: exit %d, want 2", args, code)
		}
	}
}

func TestDBCompactCommand(t *testing.T) {
	stubRetention(t, `{"BeforeBytes":1048576000,"AfterBytes":104857600}`, nil)
	var out, errb bytes.Buffer
	if code := runDB(context.Background(), []string{"compact"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "1000.0 MiB -> 100.0 MiB (freed 900.0 MiB)") {
		t.Fatalf("compact (%d): %s %s", code, out.String(), errb.String())
	}
}
