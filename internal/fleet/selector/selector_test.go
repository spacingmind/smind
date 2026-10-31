package selector

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func win(alias string, live int) Candidate {
	return Candidate{Alias: alias, HostID: "id-" + alias, Labels: map[string]string{"os": "windows"}, Enabled: true, Connected: true, Live: live}
}

func asErr(t *testing.T, err error, code string) *Error {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("want *Error, got %v", err)
	}
	if se.Code != code {
		t.Fatalf("code = %q, want %q (%v)", se.Code, code, err)
	}
	return se
}

var osWin = Selector{Labels: map[string]string{"os": "windows"}}

func TestFleetSelector_ExactOne(t *testing.T) {
	cands := []Candidate{
		{Alias: "mac", Labels: map[string]string{"os": "darwin"}, Enabled: true, Connected: true},
		win("win", 0),
		{Alias: "lin", Labels: map[string]string{"os": "linux"}, Enabled: true, Connected: true},
	}
	got, err := Select(osWin, cands)
	if err != nil || got.Alias != "win" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFleetSelector_Zero(t *testing.T) {
	_, err := Select(osWin, []Candidate{{Alias: "mac", Labels: map[string]string{"os": "darwin"}, Enabled: true, Connected: true}})
	se := asErr(t, err, CodeNoMatch)
	if !strings.Contains(se.Error(), "no host has these labels") {
		t.Fatalf("msg = %q", se.Error())
	}
	if len(se.Candidates) != 0 {
		t.Fatalf("candidates = %v", se.Candidates)
	}
}

func TestFleetSelector_Ambiguous(t *testing.T) {
	_, err := Select(osWin, []Candidate{win("b", 0), win("a", 0)})
	se := asErr(t, err, CodeAmbiguous)
	if !strings.Contains(se.Error(), "a, b") {
		t.Fatalf("msg = %q", se.Error())
	}
	if !reflect.DeepEqual(se.Candidates, []string{"a", "b"}) {
		t.Fatalf("candidates = %v", se.Candidates)
	}
}

func TestFleetSelector_AnyTieByAlias(t *testing.T) {
	sel := Selector{Labels: map[string]string{"os": "windows"}, Any: true}
	got, err := Select(sel, []Candidate{win("b", 1), win("a", 1), win("c", 0)})
	if err != nil || got.Alias != "c" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = Select(sel, []Candidate{win("b", 1), win("c", 1), win("a", 1)})
	if err != nil || got.Alias != "a" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFleetSelector_DisabledSkipped(t *testing.T) {
	off := win("off", 0)
	off.Enabled = false
	se := asErr(t, mustErr(t, []Candidate{off}), CodeNoMatch)
	if !strings.Contains(se.Error(), "off") || !strings.Contains(se.Error(), "disabled or disconnected") {
		t.Fatalf("msg = %q", se.Error())
	}
	got, err := Select(osWin, []Candidate{off, win("on", 0)})
	if err != nil || got.Alias != "on" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFleetSelector_DisconnectedSkipped(t *testing.T) {
	down := win("down", 0)
	down.Connected = false
	se := asErr(t, mustErr(t, []Candidate{down}), CodeNoMatch)
	if !strings.Contains(se.Error(), "down") || !strings.Contains(se.Error(), "disabled or disconnected") {
		t.Fatalf("msg = %q", se.Error())
	}
	got, err := Select(osWin, []Candidate{down, win("up", 0)})
	if err != nil || got.Alias != "up" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func mustErr(t *testing.T, cands []Candidate) error {
	t.Helper()
	_, err := Select(osWin, cands)
	if err == nil {
		t.Fatal("want error")
	}
	return err
}

func TestFleetSelector_EmptyLabelsInvalid(t *testing.T) {
	for _, l := range []map[string]string{nil, {}} {
		_, err := Select(Selector{Labels: l}, []Candidate{win("a", 0)})
		se := asErr(t, err, CodeInvalid)
		if se.Error() != "selector_invalid: labels must name at least one label" {
			t.Fatalf("msg = %q", se.Error())
		}
	}
}

func TestFleetSelector_NoMutation(t *testing.T) {
	sel := Selector{Labels: map[string]string{"os": "windows"}, Any: true}
	cands := []Candidate{win("b", 2), win("a", 1)}
	selCopy := Selector{Labels: map[string]string{"os": "windows"}, Any: true}
	candsCopy := []Candidate{win("b", 2), win("a", 1)}
	if _, err := Select(sel, cands); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sel, selCopy) || !reflect.DeepEqual(cands, candsCopy) {
		t.Fatal("inputs mutated")
	}
}
