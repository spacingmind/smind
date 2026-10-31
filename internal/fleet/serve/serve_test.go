package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
)

func rawReq(t *testing.T, clk *fakeClock, principal, req string, extra map[string]any) json.RawMessage {
	t.Helper()
	m := map[string]any{"prov": goodProv(clk, principal, req), "id": 7}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	var rf *Refusal
	if !errors.As(err, &rf) {
		t.Fatalf("err = %v, want *Refusal", err)
	}
	return rf.Code
}

func TestFleetReadRateLimited(t *testing.T) {
	clk := newClock()
	au := &memAudit{}
	s := New(Options{HostID: "A", Authorizer: fakeAuthorizer{grant: registry.DefaultForwardGrant()},
		Audit: au, Reader: &fakeReader{}, Now: clk.now, RatePerSec: 0.0001, Burst: 2})
	for i := range 2 {
		if _, err := s.Handle(context.Background(), "B", wire.MethodTaskGet, rawReq(t, clk, "B", fmt.Sprint("r", i), nil)); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	_, err := s.Handle(context.Background(), "B", wire.MethodTaskGet, rawReq(t, clk, "B", "r3", nil))
	if got := refusalCode(t, err); got != wire.CodeRateLimited {
		t.Fatalf("code %q", got)
	}
	if len(au.rows) != 3 || au.rows[2].Outcome != "refused" || au.rows[2].Code != wire.CodeRateLimited {
		t.Fatalf("rows %+v", au.rows)
	}
}

func TestFleetHandleAuthorizeFailureIsNotGranted(t *testing.T) {
	clk := newClock()
	au := &memAudit{}
	rd := &fakeReader{}
	s := New(Options{HostID: "A", Authorizer: fakeAuthorizer{err: errors.New("host revoked")},
		Audit: au, Reader: rd, Now: clk.now})
	_, err := s.Handle(context.Background(), "B", wire.MethodTaskGet, rawReq(t, clk, "B", "r1", nil))
	if got := refusalCode(t, err); got != wire.CodeNotGranted {
		t.Fatalf("code %q", got)
	}
	if strings.Contains(err.Error(), "revoked") {
		t.Fatalf("message leaks the reason: %v", err)
	}
	if rd.calls != 0 {
		t.Fatal("reader was called")
	}
	if len(au.rows) != 1 || au.rows[0].Code != wire.CodeNotGranted {
		t.Fatalf("rows %+v", au.rows)
	}
}

func TestFleetHandleReaderNotFoundMaps(t *testing.T) {
	clk := newClock()
	s, au, rd := newTestServer("A", clk)
	rd.err = fmt.Errorf("task 7: %w", ErrNotFound)
	_, err := s.Handle(context.Background(), "B", wire.MethodTaskGet, rawReq(t, clk, "B", "r1", nil))
	if got := refusalCode(t, err); got != wire.CodeNotFound {
		t.Fatalf("code %q", got)
	}
	if au.rows[0].Outcome != "refused" {
		t.Fatalf("row %+v", au.rows[0])
	}
}

func TestFleetHandleOtherErrorIsErrorOutcome(t *testing.T) {
	clk := newClock()
	s, au, rd := newTestServer("A", clk)
	rd.err = errors.New("boom")
	if _, err := s.Handle(context.Background(), "B", wire.MethodInfo, rawReq(t, clk, "B", "r1", nil)); err == nil {
		t.Fatal("want error")
	}
	if au.rows[0].Outcome != "error" || au.rows[0].Code != "" {
		t.Fatalf("row %+v", au.rows[0])
	}
}

func TestFleetHandleMalformedAndUnknown(t *testing.T) {
	clk := newClock()
	s, au, _ := newTestServer("A", clk)
	_, err := s.Handle(context.Background(), "B", wire.MethodInfo, json.RawMessage(`[1`))
	if got := refusalCode(t, err); got != wire.CodeInvalid {
		t.Fatalf("code %q", got)
	}
	_, err = s.Handle(context.Background(), "B", "fleet.nope", rawReq(t, clk, "B", "r1", nil))
	if got := refusalCode(t, err); got != wire.CodeInvalid {
		t.Fatalf("code %q", got)
	}
	if len(au.rows) != 2 {
		t.Fatalf("rows %d", len(au.rows))
	}
}
