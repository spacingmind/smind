package reflector

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
)

const validCreate = `{"intents":[{"action":"create","name":"learned-go-test-flags","category":"testing","description":"Use when running Go tests that hang.","body":"Run go test with -race.","old_string":null,"new_string":null,"absorbed_into":"","reason":"user corrected the flags","evidence":[{"kind":"user_message","run_id":"r1","seq":-1,"quote":"always pass -race to go test"}]}]}`

func TestReflectorOutput_RejectsNonJSONAndUnknownAction(t *testing.T) {
	longQuote := strings.Repeat("a", 501)
	ev := func(n int) string {
		one := `{"kind":"user_message","run_id":"r1","seq":1,"quote":"hello brave new world"}`
		return strings.TrimSuffix(strings.Repeat(one+",", n), ",")
	}
	intent := func(extra string) string {
		return `{"intents":[{"action":"create","name":"learned-x"` + extra + `}]}`
	}
	six := strings.TrimSuffix(strings.Repeat(`{"action":"patch","name":"learned-a"},`, 6), ",")
	cases := []struct{ name, in string }{
		{"prose", "Here are the intents: nothing."},
		{"fenced", "```json\n{\"intents\":[]}\n```"},
		{"text before", "Sure! {\"intents\":[]}"},
		{"text after", "{\"intents\":[]} done"},
		{"two objects", `{"intents":[]}{"intents":[]}`},
		{"two objects spaced", "{\"intents\":[]}\n{\"intents\":[]}"},
		{"top-level array", `[{"intents":[]}]`},
		{"missing intents", `{}`},
		{"null intents", `{"intents":null}`},
		{"unknown top field", `{"intents":[],"extra":1}`},
		{"unknown intent field", intent(`,"bogus":"x"`)},
		{"unknown action", `{"intents":[{"action":"rewrite","name":"learned-x"}]}`},
		{"empty action", `{"intents":[{"name":"learned-x"}]}`},
		{"null element", `{"intents":[null]}`},
		{"empty input", ""},
		{"whitespace only", " \n\t "},
		{"null input", "null"},
		{"over 64KB", `{"intents":[],"pad":"` + strings.Repeat("a", MaxOutputBytes) + `"}`},
		{"six intents", `{"intents":[` + six + `]}`},
		{"four evidence", intent(`,"evidence":[` + ev(4) + `]`)},
		{"501 rune quote", intent(`,"evidence":[{"kind":"user_message","run_id":"r","seq":1,"quote":"` + longQuote + `"}]`)},
		{"duplicate top key", `{"intents":[],"intents":[]}`},
		{"duplicate intent field", intent(`,"name":"learned-y"`)},
		{"duplicate evidence field", intent(`,"evidence":[{"kind":"a","kind":"b"}]`)},
		{"invalid utf8", "{\"intents\":[{\"action\":\"patch\",\"name\":\"learned-\xff\"}]}"},
		{"lone surrogate escape", `{"intents":[{"action":"patch","name":"learned-\ud800"}]}`},
		{"wrong type", `{"intents":[{"action":"patch","name":5}]}`},
		{"truncated", `{"intents":[`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseOutput(c.in, gate.ModeReflect)
			if err == nil {
				t.Fatalf("expected error, got %+v", got)
			}
			if !errors.Is(err, ErrBadOutput) {
				t.Fatalf("error does not wrap ErrBadOutput: %v", err)
			}
			if got != nil {
				t.Fatalf("partial result returned with error: %+v", got)
			}
		})
	}

	t.Run("unknown action names the action", func(t *testing.T) {
		_, err := ParseOutput(`{"intents":[{"action":"rewrite","name":"learned-x"}]}`, gate.ModeReflect)
		if err == nil || !strings.Contains(err.Error(), "rewrite") {
			t.Fatalf("want error naming action, got %v", err)
		}
	})
	t.Run("one bad intent fails the whole message", func(t *testing.T) {
		in := `{"intents":[{"action":"patch","name":"learned-a"},{"action":"nuke","name":"learned-c"}]}`
		if got, err := ParseOutput(in, gate.ModeReflect); err == nil || got != nil {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

func TestReflectorOutput_Accepts(t *testing.T) {
	patch := `{"action":"patch","name":"learned-a","category":"","description":"","body":null,"old_string":"foo","new_string":"bar","absorbed_into":"","reason":"r","evidence":[{"kind":"failure_recovery","run_id":"r1","seq":4,"quote":"retry with -count=1"}]}`
	del := `{"action":"delete","name":"learned-a","body":null,"absorbed_into":"learned-b","reason":"dup","evidence":[]}`
	curatePatch := `{"action":"patch","name":"learned-b","description":"Use when B.","old_string":"x","new_string":"y","reason":"merge","evidence":[]}`
	cases := []struct {
		name string
		mode gate.Mode
		in   string
		n    int
	}{
		{"empty", gate.ModeReflect, `{"intents":[]}`, 0},
		{"empty whitespace", gate.ModeReflect, " \n\r\n\t {\n \"intents\" : [ ]\n}\n\n ", 0},
		{"create null old_string", gate.ModeReflect, validCreate, 1},
		{"patch", gate.ModeReflect, `{"intents":[` + patch + `]}`, 1},
		{"curate patch", gate.ModeCurate, `{"intents":[` + curatePatch + `]}`, 1},
		{"curate delete absorbed", gate.ModeCurate, `{"intents":[` + del + `]}`, 1},
		{"several", gate.ModeReflect, `{"intents":[` + patch + `,` + patch + `]}`, 2},
		{"five", gate.ModeCurate, `{"intents":[` + strings.TrimSuffix(strings.Repeat(del+",", MaxIntents), ",") + `]}`, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseOutput(c.in, c.mode)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got) != c.n {
				t.Fatalf("got %#v, want %d intents (non-nil)", got, c.n)
			}
		})
	}
}

func TestParseOutput_RoundTripsFields(t *testing.T) {
	in := `{"intents":[{"action":"patch","name":"learned-a","category":"testing","description":"Use when X.","body":"b\nline2","old_string":"o","new_string":"n","absorbed_into":"learned-z","reason":"because","evidence":[{"kind":"user_message","run_id":"run-1","seq":-1,"quote":"one two three four"},{"kind":"loaded_skill_wrong","run_id":"run-1","seq":-2,"quote":"five six seven eight"}]}]}`
	got, err := ParseOutput(in, gate.ModeReflect)
	if err != nil {
		t.Fatal(err)
	}
	want := gate.Intent{
		Action: gate.ActionPatch, Name: "learned-a", Category: "testing", Description: "Use when X.",
		Body: "b\nline2", OldString: "o", NewString: "n", AbsorbedInto: "learned-z", Reason: "because",
		Evidence: []gate.Evidence{
			{Kind: "user_message", RunID: "run-1", Seq: -1, Quote: "one two three four"},
			{Kind: "loaded_skill_wrong", RunID: "run-1", Seq: -2, Quote: "five six seven eight"},
		},
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	g := got[0]
	if g.Action != want.Action || g.Name != want.Name || g.Category != want.Category || g.Description != want.Description ||
		g.Body != want.Body || g.OldString != want.OldString || g.NewString != want.NewString ||
		g.AbsorbedInto != want.AbsorbedInto || g.Reason != want.Reason || len(g.Evidence) != 2 ||
		g.Evidence[0] != want.Evidence[0] || g.Evidence[1] != want.Evidence[1] {
		t.Fatalf("got %+v\nwant %+v", g, want)
	}
}

func TestParseOutput_NullBodyIsEmpty(t *testing.T) {
	got, err := ParseOutput(validCreate, gate.ModeReflect)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].OldString != "" || got[0].NewString != "" {
		t.Fatalf("null did not decode to empty: %+v", got[0])
	}
}

func TestParseOutput_FuzzNoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	frags := []string{`{`, `}`, `[`, `]`, `"`, `,`, `:`, `null`, `"intents"`, `"action"`, "\n", `\`, `\u00`, "\xff", `-1`, `1e999`}
	for i := 0; i < 2000; i++ {
		b := []byte(validCreate)
		for m := rng.Intn(4) + 1; m > 0; m-- {
			p := rng.Intn(len(b) + 1)
			switch rng.Intn(4) {
			case 0:
				if p < len(b) {
					b[p] = byte(rng.Intn(256))
				}
			case 1:
				if p < len(b) {
					b = append(b[:p], b[p+1:]...)
				}
			case 2:
				f := frags[rng.Intn(len(frags))]
				b = append(b[:p], append([]byte(f), b[p:]...)...)
			case 3:
				b = b[:p]
			}
		}
		got, err := ParseOutput(string(b), gate.ModeReflect)
		if err != nil && len(got) != 0 {
			t.Fatalf("input %q returned %d intents and error %v", b, len(got), err)
		}
	}
}

func FuzzParseOutput(f *testing.F) {
	for _, s := range []string{validCreate, `{"intents":[]}`, ``, `{"intents":[],"intents":[]}`, "```json\n{}\n```", `{"intents":[{"action":"rewrite"}]}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := ParseOutput(s, gate.ModeReflect)
		if err != nil && got != nil {
			t.Fatalf("error with non-nil result: %v", err)
		}
		if err == nil && got == nil {
			t.Fatal("nil result without error")
		}
	})
}

func TestParseOutput_RejectsCaseFoldedKeys(t *testing.T) {
	for name, in := range map[string]string{
		"upper top":      `{"Intents":[]}`,
		"all caps top":   `{"INTENTS":[]}`,
		"kelvin sign":    "{\"intents\":[{\"action\":\"create\",\"name\":\"learned-x\",\"\u212aind\":\"x\"}]}",
		"upper action":   `{"intents":[{"ACTION":"create","name":"learned-x"}]}`,
		"mixed name":     `{"intents":[{"action":"create","Name":"learned-x"}]}`,
		"evidence upper": `{"intents":[{"action":"create","name":"learned-x","evidence":[{"Kind":"user_message"}]}]}`,
		"evidence quote": `{"intents":[{"action":"create","name":"learned-x","evidence":[{"kind":"user_message","QUOTE":"a b c"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParseOutput(in, gate.ModeReflect)
			if err == nil || !errors.Is(err, ErrBadOutput) || got != nil {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestParseOutput_ReflectRejectsDelete(t *testing.T) {
	in := `{"intents":[{"action":"delete","name":"learned-a","absorbed_into":"learned-b","reason":"dup","evidence":[]}]}`
	got, err := ParseOutput(in, gate.ModeReflect)
	if err == nil || !errors.Is(err, ErrBadOutput) || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := ParseOutput(validCreate, gate.ModeReflect); err != nil {
		t.Fatalf("create must stay valid: %v", err)
	}
}

func TestParseOutput_CurateRules(t *testing.T) {
	ev := `[{"kind":"user_message","run_id":"r","seq":-1,"quote":"one two three four"}]`
	cases := []struct {
		name, in string
		ok       bool
	}{
		{"create rejected", `{"intents":[{"action":"create","name":"learned-a","reason":"r","evidence":[]}]}`, false},
		{"patch with evidence rejected", `{"intents":[{"action":"patch","name":"learned-a","reason":"r","evidence":` + ev + `}]}`, false},
		{"delete with evidence rejected", `{"intents":[{"action":"delete","name":"learned-a","absorbed_into":"learned-b","reason":"r","evidence":` + ev + `}]}`, false},
		{"delete without absorbed_into", `{"intents":[{"action":"delete","name":"learned-a","reason":"r","evidence":[]}]}`, false},
		{"delete null absorbed_into", `{"intents":[{"action":"delete","name":"learned-a","absorbed_into":null,"reason":"r","evidence":[]}]}`, false},
		{"delete ok", `{"intents":[{"action":"delete","name":"learned-a","absorbed_into":"learned-b","reason":"r","evidence":[]}]}`, true},
		{"patch ok", `{"intents":[{"action":"patch","name":"learned-a","reason":"r","evidence":[]}]}`, true},
		{"patch missing evidence ok", `{"intents":[{"action":"patch","name":"learned-a","reason":"r"}]}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseOutput(c.in, gate.ModeCurate)
			if c.ok && (err != nil || len(got) != 1) {
				t.Fatalf("got %+v, %v", got, err)
			}
			if !c.ok && (err == nil || !errors.Is(err, ErrBadOutput) || got != nil) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestParseOutput_RejectsUnicodeWhitespaceWrapper(t *testing.T) {
	for name, ws := range map[string]string{"nbsp": "\u00a0", "ideographic": "\u3000", "line sep": "\u2028", "zero-width-ish em space": "\u2003"} {
		for _, in := range []string{ws + `{"intents":[]}`, `{"intents":[]}` + ws} {
			if got, err := ParseOutput(in, gate.ModeReflect); err == nil || !errors.Is(err, ErrBadOutput) || got != nil {
				t.Fatalf("%s: got %+v, %v", name, got, err)
			}
		}
	}
	if _, err := ParseOutput(" \t\r\n"+`{"intents":[]}`+"\n\r\t ", gate.ModeReflect); err != nil {
		t.Fatalf("ASCII whitespace must pass: %v", err)
	}
}

func TestScopeCheck_Unknown(t *testing.T) {
	shown := []string{"learned-b", "learned-a", "learned-a"}
	patch := func(n string) gate.Intent { return gate.Intent{Action: gate.ActionPatch, Name: n} }
	del := func(n, into string) gate.Intent {
		return gate.Intent{Action: gate.ActionDelete, Name: n, AbsorbedInto: into}
	}
	cases := []struct {
		name    string
		mode    gate.Mode
		intents []gate.Intent
		ok      bool
	}{
		{"patch shown", gate.ModeReflect, []gate.Intent{patch("learned-a")}, true},
		{"create any name", gate.ModeReflect, []gate.Intent{{Action: gate.ActionCreate, Name: "learned-new"}}, true},
		{"patch unknown", gate.ModeReflect, []gate.Intent{patch("learned-zzz")}, false},
		{"delete unknown name", gate.ModeCurate, []gate.Intent{patch("learned-a"), del("learned-zzz", "learned-a")}, false},
		{"absorbed unknown", gate.ModeCurate, []gate.Intent{patch("learned-zzz"), del("learned-a", "learned-zzz")}, false},
		{"mutual absorb", gate.ModeCurate, []gate.Intent{del("learned-a", "learned-b"), del("learned-b", "learned-a")}, false},
		{"absorbed deleted too", gate.ModeCurate, []gate.Intent{patch("learned-a"), del("learned-b", "learned-a"), del("learned-a", "learned-b")}, false},
		{"delete without paired patch", gate.ModeCurate, []gate.Intent{del("learned-a", "learned-b")}, false},
		{"patch of other skill is not a pair", gate.ModeCurate, []gate.Intent{patch("learned-a"), del("learned-b", "learned-c")}, false},
		{"double targeting patch", gate.ModeReflect, []gate.Intent{patch("learned-a"), patch("learned-a")}, false},
		{"double targeting patch and delete", gate.ModeCurate, []gate.Intent{patch("learned-a"), del("learned-a", "learned-b")}, false},
		{"curate pairing ok", gate.ModeCurate, []gate.Intent{patch("learned-a"), del("learned-b", "learned-a")}, true},
		{"empty", gate.ModeCurate, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ScopeCheck(c.intents, c.mode, shown)
			if c.ok && err != nil {
				t.Fatal(err)
			}
			if !c.ok && (err == nil || !errors.Is(err, ErrBadOutput)) {
				t.Fatalf("want ErrBadOutput, got %v", err)
			}
		})
	}
}
