package arm

import "testing"

func TestArmCaps_MeterEndsOnEachCap(t *testing.T) {
	cases := map[string]struct {
		do   func(*Meter) End
		want End
	}{
		"cost":              {func(m *Meter) End { m.Usage(0.4, true); return m.Usage(0.7, true) }, EndCapped},
		"unreported usage":  {func(m *Meter) End { return m.Usage(0, false) }, EndCapped},
		"output":            {func(m *Meter) End { m.Output(60); return m.Output(60) }, EndOutput},
		"turns":             {func(m *Meter) End { m.Turn(); m.Turn(); return m.Turn() }, EndTurns},
		"under every cap":   {func(m *Meter) End { m.Usage(0.1, true); m.Output(10); return m.Turn() }, ""},
		"exactly at output": {func(m *Meter) End { return m.Output(100) }, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := NewMeter(Caps{MaxTurns: 2, CostUSD: 1, OutputBytes: 100})
			if got := tc.do(m); got != tc.want {
				t.Errorf("End = %q, want %q", got, tc.want)
			}
			if m.Tripped() != tc.want {
				t.Errorf("Tripped = %q, want %q", m.Tripped(), tc.want)
			}
		})
	}
}

func TestArmCaps_FirstCapWins(t *testing.T) {
	m := NewMeter(Caps{MaxTurns: 1, CostUSD: 1, OutputBytes: 10})
	if m.Output(11) != EndOutput {
		t.Fatal("output did not trip")
	}
	if m.Usage(5, true) != "" || m.Turn() != "" || m.Turn() != "" {
		t.Error("a later cap re-tripped the meter")
	}
	if m.Tripped() != EndOutput {
		t.Errorf("Tripped = %q", m.Tripped())
	}
	if c := m.Cost(); c != 5 {
		t.Errorf("Cost = %v", c)
	}
}

func TestArmCaps_UnreportedChargesWholeCap(t *testing.T) {
	m := NewMeter(Caps{CostUSD: 0.8})
	m.Usage(0.1, true)
	m.Usage(0, false)
	if m.Cost() != 0.8 {
		t.Errorf("Cost = %v, want the cap 0.8", m.Cost())
	}
}
