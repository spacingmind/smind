package wsapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// newAccountTestServer builds a server whose accounts registry shares the
// test's store, so cascade tests can seed routing decisions/quota
// snapshots directly against the account rows the RPCs create.
func newAccountTestServer(t *testing.T) (*httptest.Server, *accounts.Registry, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	srv := newTestWSServerWithAccounts(t, wm, registry, nil, s, "tok")
	return srv, registry, s
}

func TestAccount_RenameRoundTrip(t *testing.T) {
	t.Parallel()
	srv, registry, _ := newAccountTestServer(t)
	_ = registry
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "old", "credential": "sk-ant-old",
	})
	var created accountResult
	mustDecode(t, readEnvelopeFor(t, ws, "add", 5*time.Second).Result, &created)

	sendRequest(t, ws, "rename", "account.rename", map[string]any{
		"id": created.ID, "label": "work-claude",
	})
	resp := readEnvelopeFor(t, ws, "rename", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("account.rename error = %v", resp.Error.Message)
	}
	var renamed accountResult
	mustDecode(t, resp.Result, &renamed)
	if renamed.ID != created.ID || renamed.Label != "work-claude" {
		t.Fatalf("account.rename result = %+v, want label work-claude", renamed)
	}

	sendRequest(t, ws, "list", "account.list", nil)
	resp = readEnvelopeFor(t, ws, "list", 5*time.Second)
	var listed []accountResult
	mustDecode(t, resp.Result, &listed)
	if len(listed) != 1 || listed[0].Label != "work-claude" {
		t.Fatalf("account.list after rename = %+v, want the renamed label", listed)
	}
}

func TestAccount_RenameNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "rename", "account.rename", map[string]any{"id": 999, "label": "x"})
	resp := readEnvelopeFor(t, ws, "rename", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("account.rename on unknown id error = nil, want a not-found error")
	}
}

func TestAccount_RenameEmptyLabelRejected(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "old", "credential": "sk-ant-old",
	})
	var created accountResult
	mustDecode(t, readEnvelopeFor(t, ws, "add", 5*time.Second).Result, &created)

	for _, label := range []string{"", "   "} {
		sendRequest(t, ws, "rename", "account.rename", map[string]any{"id": created.ID, "label": label})
		if resp := readEnvelopeFor(t, ws, "rename", 5*time.Second); resp.Error == nil {
			t.Fatalf("account.rename(%q) error = nil, want rejected", label)
		}
	}
}

func TestAccount_UpdateCredentialNeverEchoesCredential(t *testing.T) {
	t.Parallel()
	srv, registry, _ := newAccountTestServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "work", "credential": "sk-ant-old",
	})
	var created accountResult
	mustDecode(t, readEnvelopeFor(t, ws, "add", 5*time.Second).Result, &created)

	// oauth swap (type changes), then api_key swap with a baseUrl.
	sendRequest(t, ws, "swap", "account.updateCredential", map[string]any{
		"id":         created.ID,
		"credential": `{"access_token":"access-new","refresh_token":"refresh-new","expires_at":"2030-01-01T00:00:00Z"}`,
	})
	resp := readEnvelopeFor(t, ws, "swap", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("account.updateCredential error = %v", resp.Error.Message)
	}
	if bytes.Contains(resp.Result, []byte("refresh-new")) || bytes.Contains(resp.Result, []byte("access-new")) {
		t.Fatalf("account.updateCredential returned credential material: %s", resp.Result)
	}
	var updated accountResult
	mustDecode(t, resp.Result, &updated)
	if updated.CredentialType != accounts.CredentialTypeOAuth {
		t.Fatalf("credentialType after oauth swap = %q, want oauth", updated.CredentialType)
	}

	sendRequest(t, ws, "swap2", "account.updateCredential", map[string]any{
		"id": created.ID, "credential": "sk-ant-new", "baseUrl": "http://127.0.0.1:8080",
	})
	resp = readEnvelopeFor(t, ws, "swap2", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("account.updateCredential error = %v", resp.Error.Message)
	}
	if bytes.Contains(resp.Result, []byte("sk-ant-new")) {
		t.Fatalf("account.updateCredential returned credential material: %s", resp.Result)
	}
	mustDecode(t, resp.Result, &updated)
	if updated.CredentialType != accounts.CredentialTypeAPIKey {
		t.Fatalf("credentialType after api_key swap = %q, want api_key", updated.CredentialType)
	}

	// The swap really took: the stored credential is the new one.
	got, err := registry.Get(created.ID)
	if err != nil {
		t.Fatalf("registry.Get() error = %v", err)
	}
	if got.APIKey == nil || got.APIKey.Key != "sk-ant-new" || got.APIKey.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("stored credential = %+v, want sk-ant-new with the loopback base_url", got.APIKey)
	}
}

func TestAccount_UpdateCredentialNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "swap", "account.updateCredential", map[string]any{"id": 999, "credential": "sk-x"})
	if resp := readEnvelopeFor(t, ws, "swap", 5*time.Second); resp.Error == nil {
		t.Fatal("account.updateCredential on unknown id error = nil, want a not-found error")
	}
}

// TestAccount_RemoveCascadesAffinity is ADR-0015's condition (b) over the
// wire: after account.remove, no routing session-affinity row still points
// at the removed account, and the account is gone from account.list.
func TestAccount_RemoveCascadesAffinity(t *testing.T) {
	t.Parallel()
	srv, _, s := newAccountTestServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "add1", "account.add", map[string]any{
		"provider": "anthropic", "label": "sibling", "credential": "sk-ant-keep",
	})
	var keep accountResult
	mustDecode(t, readEnvelopeFor(t, ws, "add1", 5*time.Second).Result, &keep)
	sendRequest(t, ws, "add2", "account.add", map[string]any{
		"provider": "anthropic", "label": "kill", "credential": "sk-ant-kill",
	})
	var kill accountResult
	mustDecode(t, readEnvelopeFor(t, ws, "add2", 5*time.Second).Result, &kill)

	now := time.Now().UTC()
	for _, key := range []string{"sess-a", "sess-b"} {
		if _, err := s.CreateRoutingDecision(store.RoutingDecision{
			SessionKey: key, AccountID: kill.ID, Policy: "affinity",
			DecidedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		}); err != nil {
			t.Fatalf("CreateRoutingDecision(%s) error = %v", key, err)
		}
	}
	if _, err := s.CreateRoutingDecision(store.RoutingDecision{
		SessionKey: "sess-c", AccountID: keep.ID, Policy: "affinity",
		DecidedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateRoutingDecision(sess-c) error = %v", err)
	}
	if _, err := s.CreateQuotaSnapshot(store.QuotaSnapshot{
		AccountID: kill.ID, UsageData: "{}", PolledAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateQuotaSnapshot() error = %v", err)
	}

	sendRequest(t, ws, "rm", "account.remove", map[string]any{"id": kill.ID})
	resp := readEnvelopeFor(t, ws, "rm", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("account.remove error = %v", resp.Error.Message)
	}

	decisions, err := s.ListRoutingDecisions()
	if err != nil {
		t.Fatalf("ListRoutingDecisions() error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].SessionKey != "sess-c" {
		t.Fatalf("routing decisions after remove = %+v, want only the sibling's sess-c (no affinity row pointing at account %d)", decisions, kill.ID)
	}
	snapshots, err := s.ListQuotaSnapshots()
	if err != nil {
		t.Fatalf("ListQuotaSnapshots() error = %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("quota snapshots after remove = %+v, want empty", snapshots)
	}

	sendRequest(t, ws, "list", "account.list", nil)
	resp = readEnvelopeFor(t, ws, "list", 5*time.Second)
	var listed []accountResult
	mustDecode(t, resp.Result, &listed)
	if len(listed) != 1 || listed[0].ID != keep.ID {
		t.Fatalf("account.list after remove = %+v, want only the sibling", listed)
	}
}

func TestAccount_RemoveNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "rm", "account.remove", map[string]any{"id": 999})
	if resp := readEnvelopeFor(t, ws, "rm", 5*time.Second); resp.Error == nil {
		t.Fatal("account.remove on unknown id error = nil, want a not-found error")
	}
}

func TestEvents_AccountUpdatedOnRename(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	sendRequest(t, ec.ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "old", "credential": "sk-ant-old",
	})
	var created accountResult
	if err := json.Unmarshal(ec.nextResponse("add", 5*time.Second).Result, &created); err != nil {
		t.Fatalf("decode account.add result: %v", err)
	}

	ec.subscribe("sub", TopicAccountUpdated)
	sendRequest(t, ec.ws, "rename", "account.rename", map[string]any{"id": created.ID, "label": "new"})
	if resp := ec.nextResponse("rename", 5*time.Second); resp.Error != nil {
		t.Fatalf("account.rename error = %v", resp.Error.Message)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicAccountUpdated {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicAccountUpdated)
	}
	var p accountUpdatedPayload
	decodePayload(t, ev, &p)
	if p.Account.ID != created.ID || p.Account.Label != "new" {
		t.Fatalf("account.updated payload = %+v, want the renamed account", p.Account)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

// TestEvents_AccountUpdatedCarriesNoCredential pins the account.updated
// payload's credential-freedom against the raw payload text: no key or
// token material may ride along on the event any more than on the RPC
// response.
func TestEvents_AccountUpdatedCarriesNoCredential(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	sendRequest(t, ec.ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "work", "credential": "sk-ant-old",
	})
	var created accountResult
	if err := json.Unmarshal(ec.nextResponse("add", 5*time.Second).Result, &created); err != nil {
		t.Fatalf("decode account.add result: %v", err)
	}

	ec.subscribe("sub", TopicAccountUpdated)
	sendRequest(t, ec.ws, "swap", "account.updateCredential", map[string]any{
		"id": created.ID, "credential": "sk-ant-secret",
	})
	if resp := ec.nextResponse("swap", 5*time.Second); resp.Error != nil {
		t.Fatalf("account.updateCredential error = %v", resp.Error.Message)
	}

	ev := ec.nextEvent(5 * time.Second)
	raw, err := json.Marshal(ev.Payload)
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	if bytes.Contains(raw, []byte("sk-ant-secret")) {
		t.Fatalf("account.updated payload leaked the credential: %s", raw)
	}
}

func TestEvents_AccountRemovedSubscribeAndReceive(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	sendRequest(t, ec.ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "throwaway", "credential": "sk-ant-x",
	})
	var created accountResult
	if err := json.Unmarshal(ec.nextResponse("add", 5*time.Second).Result, &created); err != nil {
		t.Fatalf("decode account.add result: %v", err)
	}

	ec.subscribe("sub", TopicAccountRemoved)
	sendRequest(t, ec.ws, "rm", "account.remove", map[string]any{"id": created.ID})
	if resp := ec.nextResponse("rm", 5*time.Second); resp.Error != nil {
		t.Fatalf("account.remove error = %v", resp.Error.Message)
	}

	ev := ec.nextEvent(5 * time.Second)
	if ev.Topic != TopicAccountRemoved {
		t.Fatalf("event topic = %q, want %q", ev.Topic, TopicAccountRemoved)
	}
	var p accountRemovedPayload
	decodePayload(t, ev, &p)
	if p.ID != created.ID {
		t.Fatalf("account.removed payload id = %d, want %d", p.ID, created.ID)
	}
	ec.expectNoEvent(300 * time.Millisecond)
}

func TestEvents_AccountUpdatedReachesSecondClient(t *testing.T) {
	t.Parallel()
	srv, _, _ := newAccountTestServer(t)

	ecA := newEventConn(t, dialWS(t, srv, "tok"))
	ecB := newEventConn(t, dialWS(t, srv, "tok"))
	ecB.subscribe("sub", TopicAccountUpdated)

	sendRequest(t, ecA.ws, "add", "account.add", map[string]any{
		"provider": "anthropic", "label": "old", "credential": "sk-ant-old",
	})
	var created accountResult
	if err := json.Unmarshal(ecA.nextResponse("add", 5*time.Second).Result, &created); err != nil {
		t.Fatalf("decode account.add result: %v", err)
	}

	sendRequest(t, ecA.ws, "rename", "account.rename", map[string]any{"id": created.ID, "label": "new"})
	if resp := ecA.nextResponse("rename", 5*time.Second); resp.Error != nil {
		t.Fatalf("account.rename error = %v", resp.Error.Message)
	}

	ev := ecB.nextEvent(5 * time.Second)
	if ev.Topic != TopicAccountUpdated {
		t.Fatalf("ecB event topic = %q, want %q", ev.Topic, TopicAccountUpdated)
	}
	var p accountUpdatedPayload
	decodePayload(t, ev, &p)
	if p.Account.Label != "new" {
		t.Fatalf("account.updated payload label = %q, want new", p.Account.Label)
	}
}
