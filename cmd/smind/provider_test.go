package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// stubProvider points providerDial at a fake caller that returns callErr (or
// decodes result) and records each call's method and params.
func stubProvider(t *testing.T, callErr error) *[]cooldownCall {
	t.Helper()
	var seen []cooldownCall
	old := providerDial
	providerDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, params any, out any) error {
			seen = append(seen, cooldownCall{method, params})
			if callErr != nil {
				return callErr
			}
			raw, _ := json.Marshal(map[string]any{"id": "x"})
			return json.Unmarshal(raw, out)
		}, func() {}, nil
	}
	t.Cleanup(func() { providerDial = old })
	return &seen
}

func runProviderCmd(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = runProvider(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

// providerParams JSON-round-trips the recorded params so the exact compare
// sees what crossed the wire ([] stays [], not null).
func providerParams(t *testing.T, seen *[]cooldownCall) map[string]any {
	t.Helper()
	if len(*seen) != 1 {
		t.Fatalf("calls = %#v", *seen)
	}
	if (*seen)[0].method != "provider.updateInstance" {
		t.Fatalf("method = %q", (*seen)[0].method)
	}
	raw, err := json.Marshal((*seen)[0].params)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestProviderUpdateCLI(t *testing.T) {
	t.Run("models list", func(t *testing.T) {
		seen := stubProvider(t, nil)
		code, out, errOut := runProviderCmd("update", "x", "--models", "a,b")
		if code != 0 || errOut != "" || out != "Updated x.\n" {
			t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
		}
		want := map[string]any{
			"id":     "x",
			"models": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
		}
		if got := providerParams(t, seen); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %#v want %#v", got, want)
		}
	})

	t.Run("add models and disallow", func(t *testing.T) {
		seen := stubProvider(t, nil)
		code, _, errOut := runProviderCmd("update", "x", "--add-models", "glm-4.7, glm-5", "--disallow", "WebSearch,Bash(rm:*)")
		if code != 0 || errOut != "" {
			t.Fatalf("code %d stderr %q", code, errOut)
		}
		want := map[string]any{
			"id":               "x",
			"additionalModels": []any{map[string]any{"id": "glm-4.7"}, map[string]any{"id": "glm-5"}},
			"disallowedTools":  []any{"WebSearch", "Bash(rm:*)"},
		}
		if got := providerParams(t, seen); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %#v want %#v", got, want)
		}
	})

	t.Run("inherit false", func(t *testing.T) {
		seen := stubProvider(t, nil)
		code, _, errOut := runProviderCmd("update", "x", "--inherit-claude-config=false")
		if code != 0 || errOut != "" {
			t.Fatalf("code %d stderr %q", code, errOut)
		}
		want := map[string]any{"id": "x", "inheritClaudeConfig": false}
		if got := providerParams(t, seen); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %#v want %#v", got, want)
		}
	})

	t.Run("empty value clears", func(t *testing.T) {
		seen := stubProvider(t, nil)
		code, _, errOut := runProviderCmd("update", "x", "--models", "")
		if code != 0 || errOut != "" {
			t.Fatalf("code %d stderr %q", code, errOut)
		}
		want := map[string]any{"id": "x", "models": []any{}}
		if got := providerParams(t, seen); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %#v want %#v", got, want)
		}
	})

	t.Run("no flags -> exit 2 and usage on stderr and no daemon call", func(t *testing.T) {
		seen := stubProvider(t, nil)
		code, out, errOut := runProviderCmd("update", "x")
		if code != 2 || out != "" || !strings.Contains(errOut, "smind provider update <id>") {
			t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
		}
		if len(*seen) != 0 {
			t.Errorf("daemon was dialed: %#v", *seen)
		}
	})

	t.Run("unknown flag -> exit 2", func(t *testing.T) {
		seen := stubProvider(t, nil)
		for _, args := range [][]string{
			{"update", "x", "--bogus"},
			{"update", "x", "--inherit-claude-config=maybe"},
			{"update"},
			{"update", "x", "--models", "a", "extra"},
			{"ls"},
		} {
			code, out, _ := runProviderCmd(args...)
			if code != 2 || out != "" {
				t.Errorf("%v: code %d stdout %q", args, code, out)
			}
		}
		if len(*seen) != 0 {
			t.Errorf("daemon was dialed: %#v", *seen)
		}
	})

	t.Run("daemon error -> exit 1 and message on stderr", func(t *testing.T) {
		stubProvider(t, errors.New("provider.updateInstance: nothing to update"))
		code, out, errOut := runProviderCmd("update", "x", "--models", "a")
		if code != 1 || out != "" || strings.TrimSpace(errOut) != "smind: provider.updateInstance: nothing to update" {
			t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
		}
	})

	t.Run("success output \"Updated x.\"", func(t *testing.T) {
		stubProvider(t, nil)
		code, out, errOut := runProviderCmd("update", "x", "--disallow", "WebSearch")
		if code != 0 || errOut != "" || out != "Updated x.\n" {
			t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
		}
	})
}
