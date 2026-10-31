package client

import (
	"context"
	"errors"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/wsclient"
)

func observe(c *Client, ref string) (Observation, error) {
	return c.Observe(context.Background(), ref, CallOpts{}, "r1", 7)
}

func TestObservationThreeFailuresIsStatusUnknownNeverFinished(t *testing.T) {
	e, c := newEnv(t)
	e.fc.err = errors.New("boom")
	for i := 1; i <= 2; i++ {
		ob, err := observe(c, "box")
		if err == nil || ob.Failures != i || ob.Status != nil || ob.Unknown {
			t.Fatalf("attempt %d: %+v %v", i, ob, err)
		}
	}
	ob, err := observe(c, "box")
	if err != nil || !ob.Unknown || ob.Status != nil || ob.Failures != 3 {
		t.Fatalf("third: %+v %v", ob, err)
	}
}

func TestObservationConnectingNotCounted(t *testing.T) {
	e, c := newEnv(t)
	e.state = LinkConnecting
	for i := 0; i < 5; i++ {
		ob, err := observe(c, "box")
		if !IsCode(err, CodeHostUnavailable) || ob.Failures != 0 || ob.Unknown {
			t.Fatalf("connecting %d: %+v %v", i, ob, err)
		}
	}
	e.state = LinkConnected
	e.fc.err = errors.New("boom")
	for i := 1; i <= 2; i++ {
		if ob, _ := observe(c, "box"); ob.Unknown || ob.Failures != i {
			t.Fatalf("failure %d: %+v", i, ob)
		}
	}
	if ob, err := observe(c, "box"); err != nil || !ob.Unknown {
		t.Fatalf("third: %+v %v", ob, err)
	}
}

func TestObservationSuccessResets(t *testing.T) {
	e, c := newEnv(t)
	e.fc.err = errors.New("boom")
	observe(c, "box")
	observe(c, "box")
	e.fc.err = nil
	e.fc.result = `{"runId":"r1","taskId":7,"state":"running","provider":"p"}`
	ob, err := observe(c, "box")
	if err != nil || ob.Status == nil || ob.Status.State != "running" || ob.Failures != 0 {
		t.Fatalf("success: %+v %v", ob, err)
	}
	var _ *wire.RunStatus = ob.Status
	e.fc.err = errors.New("boom")
	ob, _ = observe(c, "box")
	if ob.Failures != 1 || ob.Unknown {
		t.Fatalf("after reset: %+v", ob)
	}
}

func TestObservationRemovedHostUnavailableAtOnce(t *testing.T) {
	_, c := newEnv(t)
	ob, err := observe(c, "gone")
	if !IsCode(err, CodeUnknownHost) || ob.Unknown || ob.Failures != 0 {
		t.Fatalf("%+v %v", ob, err)
	}
}

func TestObservationTypedRefusalNotCounted(t *testing.T) {
	e, c := newEnv(t)
	e.fc.err = &wsclient.RPCError{Code: wire.CodeNotGranted, Message: "no"}
	for i := 0; i < 5; i++ {
		ob, err := observe(c, "box")
		if !IsCode(err, wire.CodeNotGranted) || ob.Unknown || ob.Failures != 0 {
			t.Fatalf("attempt %d: %+v %v", i, ob, err)
		}
	}
}
