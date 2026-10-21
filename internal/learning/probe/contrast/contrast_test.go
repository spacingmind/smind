package contrast

import "testing"

func f(v float64) *float64 { return &v }

func wantKeys() []MetricKey {
	return []MetricKey{KeyToolCalls, KeyFailedToolCalls, KeyPermissionDenials, KeyTurns, KeyCostUSD, KeyDurationMs, KeyDiffBytes}
}

func TestContrast_NoiseFloor(t *testing.T) {
	base := ArmInput{Role: RoleA, Turns: 3, CostUSD: 0.10, DurationMs: 1000, DiffBytes: 100}
	b := ArmInput{Role: RoleB, Turns: 4, CostUSD: 0.12, DurationMs: 1100, DiffBytes: 5000}

	t.Run("no replicate", func(t *testing.T) {
		c := Build(base, b, nil, "learned-x", "/proj", nil)
		if c.NoiseFloor {
			t.Fatal("NoiseFloor = true, want false")
		}
		for _, r := range c.Metrics {
			if r.Noise != nil {
				t.Fatalf("%s: Noise = %v, want nil", r.Key, *r.Noise)
			}
			if r.Key != KeyDiffBytes && r.DeltaAB > 0 && !r.Worse {
				t.Fatalf("%s: Worse = false, want true (any increase is worse without a floor)", r.Key)
			}
		}
	})

	t.Run("replicate identical to A", func(t *testing.T) {
		a2 := base
		a2.Role = RoleA2
		c := Build(base, b, &a2, "learned-x", "/proj", nil)
		if !c.NoiseFloor {
			t.Fatal("NoiseFloor = false")
		}
		for _, r := range c.Metrics {
			if r.Noise == nil || *r.Noise != 0 {
				t.Fatalf("%s: Noise = %v, want 0", r.Key, r.Noise)
			}
		}
		if r := c.Metrics[3]; r.Key != KeyTurns || !r.Worse {
			t.Fatalf("turns: %+v, want Worse (4 > 3 + 0)", r)
		}
	})

	t.Run("wide noise floor", func(t *testing.T) {
		a2 := ArmInput{Role: RoleA2, Turns: 7, CostUSD: 0.30, DurationMs: 3000, DiffBytes: 400}
		c := Build(base, b, &a2, "learned-x", "/proj", nil)
		for _, r := range c.Metrics {
			if r.Key == KeyDiffBytes {
				continue
			}
			if r.Worse {
				t.Fatalf("%s: Worse = true, want false (within noise)", r.Key)
			}
		}
	})

	t.Run("diffBytes never worse", func(t *testing.T) {
		a2 := base
		a2.Role = RoleA2
		big := ArmInput{Role: RoleB, DiffBytes: 999999}
		c := Build(base, big, &a2, "learned-x", "/proj", nil)
		var row *Row
		for i, r := range c.Metrics {
			if r.Key == KeyDiffBytes {
				row = &c.Metrics[i]
			}
		}
		if row == nil || row.Worse {
			t.Fatalf("diffBytes row %+v, want present and not Worse", row)
		}
	})

	t.Run("row order", func(t *testing.T) {
		c := Build(base, b, nil, "learned-x", "/proj", nil)
		if len(c.Metrics) != len(wantKeys()) {
			t.Fatalf("got %d rows, want %d", len(c.Metrics), len(wantKeys()))
		}
		for i, k := range wantKeys() {
			if c.Metrics[i].Key != k {
				t.Fatalf("row %d = %s, want %s", i, c.Metrics[i].Key, k)
			}
		}
	})
}

func TestContrast_CheckWorse(t *testing.T) {
	pass := CheckResult{Passed: true}
	fail := CheckResult{Passed: false}
	cases := []struct {
		name      string
		a, b      *CheckResult
		wantWorse bool
	}{
		{"A pass B fail", &pass, &fail, true},
		{"both pass", &pass, &pass, false},
		{"no check", nil, nil, false},
		{"A only", &pass, nil, false},
		{"B only", nil, &fail, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := ArmInput{Role: RoleA, Check: c.a}
			b := ArmInput{Role: RoleB, Check: c.b}
			got := Build(a, b, nil, "learned-x", "/proj", nil).CheckWorse
			if got != c.wantWorse {
				t.Fatalf("CheckWorse = %v, want %v", got, c.wantWorse)
			}
		})
	}
}
