package wsapi

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/version"
)

// daemonInfoOver calls daemon.info over tr and decodes the result.
func daemonInfoOver(t *testing.T, client *pipeTransport) map[string]any {
	t.Helper()
	data, err := json.Marshal(envelope{ID: "1", Method: "daemon.info"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Send(data); err != nil {
		t.Fatalf("send: %v", err)
	}
	type reply struct {
		env envelope
		err error
	}
	ch := make(chan reply, 1)
	go func() {
		raw, err := client.Receive()
		if err != nil {
			ch <- reply{err: err}
			return
		}
		var env envelope
		ch <- reply{env: env, err: json.Unmarshal(raw, &env)}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("receive: %v", r.err)
		}
		if r.env.Error != nil {
			t.Fatalf("daemon.info error = %v", r.env.Error)
		}
		var got map[string]any
		if err := json.Unmarshal(r.env.Result, &got); err != nil {
			t.Fatalf("decode result %s: %v", r.env.Result, err)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for daemon.info")
		return nil
	}
}

func newInfoAPI(t *testing.T) *API {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return api
}

func TestDaemonInfoReturnsVersionCommitAndPlatform(t *testing.T) {
	oldV, oldC := version.Version, version.Commit
	version.Version, version.Commit = "1.2.3", "abc1234"
	t.Cleanup(func() { version.Version, version.Commit = oldV, oldC })

	before := time.Now().UTC().Add(-time.Second)
	api := newInfoAPI(t)
	api.SetManagedBy("systemd")

	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go api.serveLocal(ctx, server)
	defer client.Close()

	got := daemonInfoOver(t, client)
	want := map[string]any{
		"version":   "1.2.3",
		"commit":    "abc1234",
		"managedBy": "systemd",
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	started, err := time.Parse(time.RFC3339, got["startedAt"].(string))
	if err != nil || started.Before(before) || started.After(time.Now().Add(time.Second)) {
		t.Errorf("startedAt = %v (err %v), want roughly now", got["startedAt"], err)
	}
	// No secrets: the result is exactly these fields and nothing else.
	if len(got) != len(want)+1 {
		t.Errorf("daemon.info returned unexpected fields: %v", got)
	}
}

func TestDaemonInfoOmitsManagedByAndCommitWhenUnset(t *testing.T) {
	oldC := version.Commit
	version.Commit = ""
	t.Cleanup(func() { version.Commit = oldC })

	api := newInfoAPI(t)
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go api.serveLocal(ctx, server)
	defer client.Close()

	got := daemonInfoOver(t, client)
	for _, k := range []string{"managedBy", "commit"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s must be absent when unset, got %v", k, got)
		}
	}
}

// TestDaemonInfoIsReadableOverRelayChannel: a relay-served (paired device)
// session is not local, yet must read daemon.info -- it is how the desktop
// app learns a relay daemon's version, since /healthz is out of reach.
func TestDaemonInfoIsReadableOverRelayChannel(t *testing.T) {
	api := newInfoAPI(t)
	api.SetManagedBy("launchd")

	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go api.ServeTransport(ctx, server) // the relay path: local == false
	defer client.Close()

	got := daemonInfoOver(t, client)
	if got["version"] != version.Version || got["managedBy"] != "launchd" {
		t.Fatalf("daemon.info over the relay channel = %v", got)
	}
}

// TestUnknownMethodGetsAnErrorResponse: a client probing for a method this
// daemon lacks must be told so, not left waiting (it used to be dropped).
func TestUnknownMethodGetsAnErrorResponse(t *testing.T) {
	api := newInfoAPI(t)
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go api.ServeTransport(ctx, server)
	defer client.Close()

	data, _ := json.Marshal(envelope{ID: "7", Method: "no.such.method"})
	if err := client.Send(data); err != nil {
		t.Fatal(err)
	}
	type result struct {
		env envelope
		err error
	}
	ch := make(chan result, 1)
	go func() {
		raw, err := client.Receive()
		var env envelope
		if err == nil {
			err = json.Unmarshal(raw, &env)
		}
		ch <- result{env, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.env.ID != "7" || r.env.Error == nil || !strings.Contains(r.env.Error.Message, "method not found: no.such.method") {
			t.Fatalf("response = %+v, want a method-not-found error for id 7", r.env)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no response to an unknown method: the error was dropped")
	}
}
