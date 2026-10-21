package verdict

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/probe/contrast"
)

func b(v bool) *bool { return &v }

// clean builds a contrast for a helped-shaped input: nothing worse, no
// check, recurrence A=true, A2=true, B=false.
func clean(loaded bool) contrast.Contrast {
	return contrast.Contrast{
		Loaded:     loaded,
		NoiseFloor: true,
		Recurred: map[contrast.Role]*bool{
			contrast.RoleA:  b(true),
			contrast.RoleA2: b(true),
			contrast.RoleB:  b(false),
		},
	}
}

// baseInput is the full three-arm helped input.
func baseInput() Input {
	return Input{
		HasSignature: true,
		Contrast:     clean(true),
		Ends: map[contrast.Role]ArmEnd{
			contrast.RoleA: EndDone, contrast.RoleA2: EndDone, contrast.RoleB: EndDone,
		},
		Planned: []contrast.Role{contrast.RoleA, contrast.RoleA2, contrast.RoleB},
	}
}

func markWorse(c contrast.Contrast, key contrast.MetricKey) contrast.Contrast {
	for i, r := range c.Metrics {
		if r.Key == key {
			c.Metrics[i].Worse = true
		}
	}
	return c
}

func withRows(c contrast.Contrast) contrast.Contrast {
	for _, k := range []contrast.MetricKey{
		contrast.KeyToolCalls, contrast.KeyFailedToolCalls, contrast.KeyPermissionDenials,
		contrast.KeyTurns, contrast.KeyCostUSD, contrast.KeyDurationMs, contrast.KeyDiffBytes,
	} {
		c.Metrics = append(c.Metrics, contrast.Row{Key: k})
	}
	return c
}

func TestVerdict_Table(t *testing.T) {
	in := baseInput()
	quick := baseInput()
	quick.Quick = true
	quick.Contrast.NoiseFloor = false
	unsafe := baseInput()
	unsafe.Unsafe = true
	quickUnsafe := unsafe
	quickUnsafe.Quick = true
	missing := baseInput()
	delete(missing.Ends, contrast.RoleB)
	killed := baseInput()
	killed.Ends[contrast.RoleA] = EndKilled
	capped := baseInput()
	capped.Ends[contrast.RoleB] = EndCapped
	cappedUnsafe := capped
	cappedUnsafe.Unsafe = true
	cappedQuick := capped
	cappedQuick.Quick = true
	notLoaded := baseInput()
	notLoaded.Contrast.Loaded = false
	notLoadedQuick := notLoaded
	notLoadedQuick.Quick = true

	hurtFailed := baseInput()
	hurtFailed.Contrast = markWorse(withRows(clean(true)), contrast.KeyFailedToolCalls)
	hurtDenied := baseInput()
	hurtDenied.Contrast = markWorse(withRows(clean(true)), contrast.KeyPermissionDenials)
	hurtCheck := baseInput()
	hurtCheck.Contrast.CheckWorse = true
	hurtRecur := baseInput()
	hurtRecur.Contrast.Recurred[contrast.RoleA] = b(false)
	hurtRecur.Contrast.Recurred[contrast.RoleB] = b(true)
	helpedRows := withRows(clean(true))
	helpedRowsWorse := helpedRows
	helpedRowsWorse.Metrics[4].Worse = true // costUSD
	helpedDiffWorse := withRows(clean(true))
	helpedDiffWorse.Metrics[6].Worse = true // diffBytes, informational

	noSig := baseInput()
	noSig.HasSignature = false
	noSig.Contrast = withRows(clean(true))
	_ = noSig

	notInA := baseInput()
	notInA.Contrast.Recurred[contrast.RoleA] = b(false)
	notInA2 := baseInput()
	notInA2.Contrast.Recurred[contrast.RoleA2] = b(false)
	controlsDisagree := baseInput()
	controlsDisagree.Contrast.Recurred[contrast.RoleA] = b(false)
	controlsDisagree.Contrast.Recurred[contrast.RoleA2] = b(false)

	cases := []struct {
		name string
		in   Input
		want Code
	}{
		{"helped (all preconditions)", in, Helped},
		{"unsafe beats everything", unsafe, Unsafe},
		{"unsafe beats quick", quickUnsafe, Unsafe},
		{"probe_failed beats capped", func() Input {
			x := cappedInput()
			delete(x.Ends, contrast.RoleA2)
			return x
		}(), ProbeFailed},
		{"unsafe beats capped", cappedUnsafe, Unsafe},
		{"capped beats never_loaded", capped, Inconclusive},
		{"capped beats quick", cappedQuick, Inconclusive},
		{"missing arm is probe_failed", missing, ProbeFailed},
		{"killed arm is probe_failed", killed, ProbeFailed},
		{"never_loaded beats quick", notLoadedQuick, NeverLoaded},
		{"quick caps helped to inconclusive", quick, Inconclusive},
		{"hurt beats helped (failedToolCalls worse)", hurtFailed, Hurt},
		{"hurt (permissionDenials worse)", hurtDenied, Hurt},
		{"hurt (check worse)", hurtCheck, Hurt},
		{"hurt (recurred only in B)", hurtRecur, Hurt},
		{"helped needs signature (removed -> no_effect)", func() Input {
			x := baseInput()
			x.HasSignature = false
			return x
		}(), NoEffect},
		{"helped needs A recurrence (removed -> hurt)", hurtRecur2(), Hurt},
		{"helped needs A2 recurrence (removed -> inconclusive controls disagree)", notInA2, Inconclusive},
		{"helped needs B non-recurrence (removed -> no_effect)", func() Input {
			x := baseInput()
			x.Contrast.Recurred[contrast.RoleB] = b(true)
			return x
		}(), NoEffect},
		{"helped needs no worse row (removed -> inconclusive)", helpedRowsWorse2(), Inconclusive},
		{"helped ignores diffBytes worse", func() Input {
			x := baseInput()
			x.Contrast = helpedDiffWorse
			return x
		}(), Helped},
		{"no_effect for user_message kind", noSig2(), NoEffect},
		{"inconclusive when failure not in A", notInA, Inconclusive},
		{"inconclusive when controls disagree", controlsDisagree, Inconclusive},
		{"inconclusive when B worse on cost", helpedRowsWorse3(), Inconclusive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(c.in).Code; got != c.want {
				t.Fatalf("Decide = %s, want %s", got, c.want)
			}
		})
	}
	_ = helpedRows
}

func hurtFailedInput() Input {
	x := baseInput()
	x.Contrast = markWorse(withRows(clean(true)), contrast.KeyFailedToolCalls)
	return x
}

func hurtDeniedInput() Input {
	x := baseInput()
	x.Contrast = markWorse(withRows(clean(true)), contrast.KeyPermissionDenials)
	return x
}

func hurtCheckInput() Input {
	x := baseInput()
	x.Contrast.CheckWorse = true
	return x
}

func notInAInput() Input {
	x := baseInput()
	x.Contrast.Recurred[contrast.RoleA] = b(false)
	return x
}

func notInA2Input() Input {
	x := baseInput()
	x.Contrast.Recurred[contrast.RoleA2] = b(false)
	return x
}

func controlsDisagreeInput() Input {
	x := baseInput()
	x.Contrast.Recurred[contrast.RoleA] = b(false)
	x.Contrast.Recurred[contrast.RoleA2] = b(false)
	return x
}

func unsafeInput() Input {
	x := baseInput()
	x.Unsafe = true
	return x
}

func missingInput() Input {
	x := baseInput()
	delete(x.Ends, contrast.RoleB)
	return x
}

func killedInput() Input {
	x := baseInput()
	x.Ends[contrast.RoleA] = EndKilled
	return x
}

func cappedInput() Input {
	x := baseInput()
	x.Ends[contrast.RoleB] = EndCapped
	return x
}

func notLoadedInput() Input {
	x := baseInput()
	x.Contrast.Loaded = false
	return x
}

func notLoadedQuickInput() Input {
	x := notLoadedInput()
	x.Quick = true
	return x
}

func quickInput() Input {
	x := baseInput()
	x.Quick = true
	x.Contrast.NoiseFloor = false
	return x
}

func hurtRecur2() Input {
	x := baseInput()
	x.Contrast.Recurred[contrast.RoleA] = b(false)
	x.Contrast.Recurred[contrast.RoleB] = b(true)
	return x
}

func helpedRowsWorse2() Input {
	x := baseInput()
	x.Contrast = helpedRowsWorse3Contrast()
	return x
}

func helpedRowsWorse3() Input {
	x := baseInput()
	x.Contrast = helpedRowsWorse3Contrast()
	return x
}

func helpedRowsWorse3Contrast() contrast.Contrast {
	return markWorse(withRows(clean(true)), contrast.KeyCostUSD)
}

func noSig2() Input {
	x := baseInput()
	x.HasSignature = false
	x.Contrast = withRows(clean(true))
	return x
}

func TestVerdict_QuickCappedInconclusive(t *testing.T) {
	quick := func(in Input) Input {
		in.Quick = true
		return in
	}
	ins := map[string]Input{
		"would-be helped":    baseInput(),
		"would-be no_effect": noSig2(),
		"would-be hurt":      hurtRecur2(),
	}
	for name, in := range ins {
		full := Decide(in).Code
		if got := Decide(quick(in)).Code; got != Inconclusive {
			t.Errorf("%s: quick Decide = %s (full %s), want inconclusive", name, got, full)
		}
	}
}

func TestVerdict_HelpedUnreachableWithoutSignature(t *testing.T) {
	// Every non-structural input shape, all with HasSignature=false: none
	// yields helped.
	shapes := []Input{
		noSig2(),
		func() Input { x := noSig2(); x.Contrast.Loaded = false; return x }(),
		func() Input { x := noSig2(); x.Contrast.CheckWorse = true; return x }(),
		func() Input {
			x := noSig2()
			x.Contrast = markWorse(withRows(clean(true)), contrast.KeyFailedToolCalls)
			return x
		}(),
		func() Input { x := noSig2(); x.Contrast.Recurred[contrast.RoleB] = b(true); return x }(),
	}
	for i, in := range shapes {
		if got := Decide(in).Code; got == Helped {
			t.Fatalf("shape %d: helped without signature", i)
		}
	}
}

func TestVerdict_ReasonIsOneSentence(t *testing.T) {
	ins := []Input{
		baseInput(), quickInput(), unsafeInput(), missingInput(), killedInput(), cappedInput(),
		notLoadedInput(), notLoadedQuickInput(), hurtFailedInput(), hurtDeniedInput(), hurtCheckInput(), hurtRecur2(),
		notInAInput(), notInA2Input(), controlsDisagreeInput(), helpedRowsWorse3(), noSig2(),
	}
	for i, in := range ins {
		r := Decide(in)
		if r.Reason == "" || strings.Contains(r.Reason, "\n") || !strings.HasSuffix(r.Reason, ".") {
			t.Errorf("input %d (%s): reason %q is not one sentence", i, r.Code, r.Reason)
		}
	}
}
