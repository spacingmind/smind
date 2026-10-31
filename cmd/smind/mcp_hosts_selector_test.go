package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

// selectorFake answers remote.select, remote.read and remote.write per method.
type selectorFake struct {
	answers map[string]any
	errs    map[string]error
	calls   []fakeRemoteCall
}

func (f *selectorFake) Call(_ context.Context, method string, params, out any) error {
	p, _ := params.(map[string]any)
	f.calls = append(f.calls, fakeRemoteCall{method: method, params: p})
	if err := f.errs[method]; err != nil {
		return err
	}
	raw, err := json.Marshal(f.answers[method])
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (f *selectorFake) count(method string) int {
	n := 0
	for _, c := range f.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func useSelectorFake(t *testing.T, f *selectorFake) {
	t.Helper()
	old := mcpRemoteCaller
	mcpRemoteCaller = func(*wsclient.Client) mcpCaller { return f }
	t.Cleanup(func() { mcpRemoteCaller = old })
}

func winSelector() map[string]any {
	return map[string]any{"labels": map[string]any{"os": "windows"}}
}

func selectAnswer() map[string]any { return map[string]any{"host": "winbox", "hostId": "h9"} }

func TestFleetSelectorMCP_ResolvesToAliasThenReads(t *testing.T) {
	f := &selectorFake{answers: map[string]any{
		"remote.select": selectAnswer(),
		"remote.read":   map[string]any{"host": "winbox", "hostId": "h9", "op": "tasks", "data": map[string]any{"tasks": []map[string]any{}}},
	}}
	useSelectorFake(t, f)
	_, out, err := mcpTaskList(nil)(context.Background(), nil, taskListInput{Host: winSelector()})
	if err != nil {
		t.Fatal(err)
	}
	if out.Host != "winbox" {
		t.Fatalf("out.Host = %q", out.Host)
	}
	if len(f.calls) != 2 || f.calls[0].method != "remote.select" || f.calls[1].method != "remote.read" {
		t.Fatalf("calls = %+v", f.calls)
	}
	if f.calls[1].params["host"] != "winbox" {
		t.Fatalf("read params = %+v", f.calls[1].params)
	}
}

func TestFleetSelectorMCP_AliasStringUnchanged(t *testing.T) {
	f := &selectorFake{answers: map[string]any{
		"remote.read": map[string]any{"host": "mac", "hostId": "h1", "op": "tasks", "data": map[string]any{"tasks": []map[string]any{}}},
	}}
	useSelectorFake(t, f)
	if _, _, err := mcpTaskList(nil)(context.Background(), nil, taskListInput{Host: "mac"}); err != nil {
		t.Fatal(err)
	}
	if f.count("remote.select") != 0 || f.count("remote.read") != 1 || f.calls[0].params["host"] != "mac" {
		t.Fatalf("calls = %+v", f.calls)
	}
}

func TestFleetSelectorMCP_ErrorNamesCandidates(t *testing.T) {
	for _, tc := range []struct{ code, msg, want string }{
		{"selector_ambiguous", "selector_ambiguous: 2 hosts match os:windows: a, b; add labels to narrow it or pass any:true to take the least busy", "a, b"},
		{"selector_no_match", "selector_no_match: no host matches os:windows; connected: a, b", "a, b"},
	} {
		f := &selectorFake{errs: map[string]error{"remote.select": &wsclient.RPCError{Code: tc.code, Message: tc.msg}}}
		useSelectorFake(t, f)
		_, _, err := mcpTaskList(nil)(context.Background(), nil, taskListInput{Host: winSelector()})
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Count(err.Error(), tc.code) != 1 {
			t.Fatalf("%s: err = %v", tc.code, err)
		}
		if f.count("remote.read") != 0 {
			t.Fatalf("%s: read after failed select: %+v", tc.code, f.calls)
		}
	}
}

func TestFleetSelectorMCP_BadShape(t *testing.T) {
	for name, host := range map[string]any{
		"labels missing":    map[string]any{"any": true},
		"labels empty":      map[string]any{"labels": map[string]any{}},
		"labels value type": map[string]any{"labels": map[string]any{"os": 1.0}},
		"any not bool":      map[string]any{"labels": map[string]any{"os": "windows"}, "any": "yes"},
		"extra key":         map[string]any{"labels": map[string]any{"os": "windows"}, "region": "eu"},
		"number host":       3.0,
	} {
		f := &selectorFake{}
		useSelectorFake(t, f)
		_, _, err := mcpTaskList(nil)(context.Background(), nil, taskListInput{Host: host})
		if err == nil || !strings.Contains(err.Error(), "host selector") {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: calls = %+v", name, f.calls)
		}
	}
}

func TestFleetSelectorMCP_NoRetryOnWriteFailure(t *testing.T) {
	f := &selectorFake{
		answers: map[string]any{"remote.select": selectAnswer()},
		errs:    map[string]error{"remote.write": errors.New("link down")},
	}
	useSelectorFake(t, f)
	in := taskNewInput{WorkspaceID: 3, Title: "T", Host: winSelector(), Provider: "glm", Model: "m", Prompt: "p"}
	if _, _, err := mcpTaskNew(nil)(context.Background(), nil, in); err == nil {
		t.Fatal("want error")
	}
	if f.count("remote.select") != 1 || f.count("remote.write") != 1 || len(f.calls) != 2 {
		t.Fatalf("calls = %+v", f.calls)
	}
	if f.calls[1].params["host"] != "winbox" {
		t.Fatalf("write params = %+v", f.calls[1].params)
	}
}

func TestFleetSelectorMCP_AnyForwarded(t *testing.T) {
	f := &selectorFake{answers: map[string]any{
		"remote.select": selectAnswer(),
		"remote.read":   map[string]any{"host": "winbox", "op": "tasks", "data": map[string]any{}},
	}}
	useSelectorFake(t, f)
	host := map[string]any{"labels": map[string]any{"os": "windows", "gpu": "yes"}, "any": true}
	if _, _, err := mcpTaskList(nil)(context.Background(), nil, taskListInput{Host: host}); err != nil {
		t.Fatal(err)
	}
	p := f.calls[0].params
	labels, _ := p["labels"].(map[string]any)
	if f.calls[0].method != "remote.select" || p["any"] != true || len(labels) != 2 || labels["os"] != "windows" || labels["gpu"] != "yes" {
		t.Fatalf("select params = %+v", p)
	}
}
