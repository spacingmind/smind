package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/wire"
)

func (e *fleetEnv) labelsCall(t *testing.T, rc *requestContext, params any) (fleetHostLabelsResult, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := e.api.hs["fleet.host.labels"](context.Background(), rc, raw)
	if err != nil {
		return fleetHostLabelsResult{}, err
	}
	return out.(fleetHostLabelsResult), nil
}

func TestFleetHostLabels_SetMergeRemove(t *testing.T) {
	e := newFleetEnv(t)
	e.pairApproved(t, newCallerKey(t), "peer")
	rc := &requestContext{local: true}

	res, err := e.labelsCall(t, rc, map[string]any{"host": "peer"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Labels == nil || len(res.Labels) != 0 || res.Host != "peer" {
		t.Fatalf("read = %+v, want empty non-nil labels", res)
	}
	if _, err := e.labelsCall(t, rc, map[string]any{"host": "peer", "set": map[string]string{"os": "windows", "gpu": "no"}}); err != nil {
		t.Fatal(err)
	}
	res, err = e.labelsCall(t, rc, map[string]any{"host": "peer", "set": map[string]string{"gpu": "yes"}, "remove": []string{"os", "absent"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (map[string]string{"gpu": "yes"}); !reflect.DeepEqual(res.Labels, want) {
		t.Fatalf("labels = %v, want %v", res.Labels, want)
	}
	h, err := e.reg.ByAlias("peer")
	if err != nil || !reflect.DeepEqual(h.Labels, map[string]string{"gpu": "yes"}) {
		t.Fatalf("stored = %v, %v", h.Labels, err)
	}
	v, err := e.api.fleet.view(h)
	if err != nil || v.Labels["gpu"] != "yes" {
		t.Fatalf("view labels = %v, %v", v.Labels, err)
	}
}

func TestFleetHostLabels_ValidationRefused(t *testing.T) {
	e := newFleetEnv(t)
	e.pairApproved(t, newCallerKey(t), "peer")
	rc := &requestContext{local: true}

	many := map[string]string{}
	for i := 0; i < 17; i++ {
		many[fmt.Sprintf("k%d", i)] = "v"
	}
	for name, set := range map[string]map[string]string{
		"too many": many,
		"bad key":  {"API-Token": "x"},
	} {
		_, err := e.labelsCall(t, rc, map[string]any{"host": "peer", "set": set})
		var ce *rpcCodeError
		if !errors.As(err, &ce) || ce.code != codeInvalid {
			t.Fatalf("%s: error = %v, want %s", name, err, codeInvalid)
		}
		if !strings.Contains(err.Error(), "labels") {
			t.Errorf("%s: message %q lacks the validation text", name, err)
		}
	}
	if h, _ := e.reg.ByAlias("peer"); len(h.Labels) != 0 {
		t.Fatalf("refused set stored %v", h.Labels)
	}
}

func TestFleetHostLabels_LocalOnlyNotHostPrincipal(t *testing.T) {
	e := newFleetEnv(t)
	e.pairApproved(t, newCallerKey(t), "peer")
	_, err := e.labelsCall(t, &requestContext{local: false}, map[string]any{"host": "peer", "set": map[string]string{"a": "b"}})
	var ce *rpcCodeError
	if !errors.As(err, &ce) || ce.code != codeForbidden {
		t.Fatalf("non-local: error = %v, want %s", err, codeForbidden)
	}
	err = (&conn{runScoped: true}).gate("fleet.host.labels")
	if !errors.As(err, &ce) || ce.code != codeForbidden {
		t.Fatalf("run-scoped gate: error = %v, want %s", err, codeForbidden)
	}
	if h, _ := e.reg.ByAlias("peer"); len(h.Labels) != 0 {
		t.Fatalf("refused call stored %v", h.Labels)
	}
}

func TestFleetHostLabels_NeverOnWire(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(hello.Hello{}),
		reflect.TypeOf(wire.Info{}),
		reflect.TypeOf(wire.Task{}),
		reflect.TypeOf(wire.TaskCreateParams{}),
		reflect.TypeOf(wire.TaskCreateResult{}),
		reflect.TypeOf(wire.TaskSendParams{}),
		reflect.TypeOf(wire.TaskStopParams{}),
		reflect.TypeOf(wire.RunWaitParams{}),
		reflect.TypeOf(wire.Origin{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			for _, n := range []string{f.Name, f.Tag.Get("json")} {
				if strings.Contains(strings.ToLower(n), "label") {
					t.Errorf("%s.%s (%q) carries a label; labels are caller-local", typ.Name(), f.Name, n)
				}
			}
		}
	}
}
