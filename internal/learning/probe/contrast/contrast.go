package contrast

import (
	"github.com/spacingmind/smind/internal/learning/probe/signature"
)

// MetricKey identifies one row of the contrast table.
type MetricKey string

// Metric keys, in the fixed order of the Metrics rows.
const (
	KeyToolCalls         MetricKey = "toolCalls"
	KeyFailedToolCalls   MetricKey = "failedToolCalls"
	KeyPermissionDenials MetricKey = "permissionDenials"
	KeyTurns             MetricKey = "turns"
	KeyCostUSD           MetricKey = "costUSD"
	KeyDurationMs        MetricKey = "durationMs"
	KeyDiffBytes         MetricKey = "diffBytes"
)

// Row is one metric contrasted across arms. DeltaAB = B - A. Noise = |A2 - A|
// when a replicate ran, else nil. Worse means B exceeds A beyond the noise
// floor; diffBytes is informational and never worse.
type Row struct {
	Key     MetricKey
	A       float64
	A2      *float64
	B       float64
	DeltaAB float64
	Noise   *float64
	Worse   bool
}

// Contrast is the deterministic A/A'/B contrast of one probe replay.
type Contrast struct {
	Loaded     bool           // B loaded the skill
	NoiseFloor bool           // A2 present
	Metrics    []Row          // fixed order as listed
	Recurred   map[Role]*bool // per arm; nil value = no signature
	CheckWorse bool           // check present in A and B, A passed, B failed
}

// rowOrder is the fixed order of the table rows.
var rowOrder = []struct {
	key  MetricKey
	get  func(Metrics) float64
	hurt bool // counts towards Worse (diffBytes does not)
}{
	{KeyToolCalls, func(m Metrics) float64 { return float64(m.ToolCalls) }, true},
	{KeyFailedToolCalls, func(m Metrics) float64 { return float64(m.FailedToolCalls) }, true},
	{KeyPermissionDenials, func(m Metrics) float64 { return float64(m.PermissionDenials) }, true},
	{KeyTurns, func(m Metrics) float64 { return float64(m.Turns) }, true},
	{KeyCostUSD, func(m Metrics) float64 { return m.CostUSD }, true},
	{KeyDurationMs, func(m Metrics) float64 { return float64(m.DurationMs) }, true},
	{KeyDiffBytes, func(m Metrics) float64 { return float64(m.DiffBytes) }, false},
}

// Build contrasts the arms. projectionDirB is B's projection dir (only B can
// load the candidate); sig is the failure signature, nil for kinds without
// one.
func Build(a, b ArmInput, a2 *ArmInput, skill, projectionDirB string, sig *signature.Signature) Contrast {
	c := Contrast{
		Loaded: Loaded(b.Events, skill, projectionDirB),
		Recurred: map[Role]*bool{
			a.Role: FailureRecurred(a.Events, sig),
			b.Role: FailureRecurred(b.Events, sig),
		},
	}
	am, bm := ComputeMetrics(a), ComputeMetrics(b)
	var a2m *Metrics
	if a2 != nil {
		c.NoiseFloor = true
		m := ComputeMetrics(*a2)
		a2m = &m
		c.Recurred[a2.Role] = FailureRecurred(a2.Events, sig)
	}
	for _, ro := range rowOrder {
		av, bv := ro.get(am), ro.get(bm)
		row := Row{Key: ro.key, A: av, B: bv, DeltaAB: bv - av}
		var noise float64
		if a2m != nil {
			n := ro.get(*a2m) - av
			if n < 0 {
				n = -n
			}
			noise = n
			row.Noise = &n
		}
		// Worse: B strictly above A + noiseOrZero. For the integer count
		// keys a strictly greater difference is necessarily >= 1.
		row.Worse = ro.hurt && bv > av+noise
		c.Metrics = append(c.Metrics, row)
	}
	c.CheckWorse = a.Check != nil && b.Check != nil && a.Check.Passed && !b.Check.Passed
	return c
}
