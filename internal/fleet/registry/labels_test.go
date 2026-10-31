package registry

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestFleetLabelsValidate(t *testing.T) {
	many := func(n int) map[string]string {
		m := map[string]string{}
		for i := 0; i < n; i++ {
			m[fmt.Sprintf("k%d", i)] = "v"
		}
		return m
	}
	long := strings.Repeat("a", 33)
	bad := map[string]map[string]string{
		"17 labels":      many(17),
		"upper key":      {"OS": "linux"},
		"digit key":      {"9x": "linux"},
		"long key":       {long: "linux"},
		"secret-ish key": {"api-token": "x"},
		"upper value":    {"os": "Windows"},
		"long value":     {"os": long},
		"space value":    {"os": "a b"},
	}
	for name, m := range bad {
		if err := ValidateLabels(m); err == nil {
			t.Errorf("%s: accepted, want error", name)
		}
	}
	if err := ValidateLabels(many(16)); err != nil {
		t.Errorf("16 labels: %v", err)
	}
	if err := ValidateLabels(map[string]string{"os": "windows", "arch": "arm64", "gpu": "cuda"}); err != nil {
		t.Errorf("valid set: %v", err)
	}
	if err := ValidateLabels(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
}

func TestFleetLabelsSetAndList(t *testing.T) {
	r := openRegistry(t, 16)
	h, err := r.PairAsInitiator(testKey(1), "peer", "", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if h.Labels != nil {
		t.Fatalf("fresh labels = %v, want none", h.Labels)
	}
	want := map[string]string{"os": "windows", "arch": "arm64"}
	if err := r.SetLabels(h.HostID, want); err != nil {
		t.Fatal(err)
	}
	list, err := r.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	got, err := r.Get(h.HostID)
	if err != nil {
		t.Fatal(err)
	}
	byAlias, err := r.ByAlias("peer")
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]map[string]string{"List": list[0].Labels, "Get": got.Labels, "ByAlias": byAlias.Labels} {
		if !reflect.DeepEqual(g, want) {
			t.Errorf("%s labels = %v, want %v", name, g, want)
		}
	}

	if err := r.SetLabels(h.HostID, map[string]string{"OS": "x"}); err == nil {
		t.Fatal("invalid labels accepted")
	}
	got, _ = r.Get(h.HostID)
	if !reflect.DeepEqual(got.Labels, want) {
		t.Fatalf("labels after invalid set = %v, want unchanged %v", got.Labels, want)
	}

	if err := r.SetLabels(h.HostID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Get(h.HostID)
	if got.Labels != nil {
		t.Fatalf("labels after clear = %v", got.Labels)
	}
	requireErrIs(t, r.SetLabels("nope", want), ErrNotFound, "SetLabels unknown host")
}

func TestFleetLabelsSurviveRenameAndApprove(t *testing.T) {
	r := openRegistry(t, 16)
	h, err := r.AcceptPairRequest(testKey(2), "peer", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"os": "linux"}
	if err := r.SetLabels(h.HostID, want); err != nil {
		t.Fatal(err)
	}
	if err := r.Rename(h.HostID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := r.Approve(h.HostID, Grant{Ops: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(h.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Alias != "renamed" || got.State == StatePending {
		t.Fatalf("host = %+v, want renamed and not pending", got)
	}
	if !reflect.DeepEqual(got.Labels, want) {
		t.Fatalf("labels = %v, want %v", got.Labels, want)
	}
}
