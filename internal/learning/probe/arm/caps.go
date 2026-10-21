package arm

import "sync"

// End is why an arm run stopped (ADR-0037 §5, R4). EndDone is a normal end;
// every other value ends the arm early and is recorded on the arm row.
type End string

const (
	EndDone    End = "done"
	EndTimeout End = "timeout" // wall clock
	EndTurns   End = "turns"   // turn cap
	EndCapped  End = "capped"  // cost cap
	EndOutput  End = "output"  // output byte cap
	EndKilled  End = "killed"  // cancelled by a human or the daemon
	EndError   End = "error"
)

// Caps are one arm's limits. A zero field means "no cap of that kind" only
// for OutputBytes; the probe service always sets the others.
type Caps struct {
	MaxTurns    int
	CostUSD     float64
	OutputBytes int64
}

// Meter tracks an arm against its Caps. It is safe for concurrent use: usage
// events and output chunks arrive on different goroutines.
type Meter struct {
	caps Caps

	mu     sync.Mutex
	cost   float64
	output int64
	turns  int
	end    End
}

// NewMeter returns a meter for caps.
func NewMeter(caps Caps) *Meter { return &Meter{caps: caps} }

// trip records the first cap hit and reports whether this call hit it.
func (m *Meter) trip(e End) bool {
	if m.end != "" {
		return false
	}
	m.end = e
	return true
}

// Usage adds a usage event. reported=false means the provider did not report
// a cost: the arm is charged its whole cost cap (ADR-0037 §5), which ends it.
// It returns the cap that this call tripped, or "".
func (m *Meter) Usage(costUSD float64, reported bool) End {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !reported {
		m.cost = m.caps.CostUSD
	} else {
		m.cost += costUSD
	}
	if m.caps.CostUSD > 0 && m.cost >= m.caps.CostUSD && m.trip(EndCapped) {
		return EndCapped
	}
	return ""
}

// Output adds n bytes of agent output (text, tool input and results).
func (m *Meter) Output(n int) End {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.output += int64(n)
	if m.caps.OutputBytes > 0 && m.output > m.caps.OutputBytes && m.trip(EndOutput) {
		return EndOutput
	}
	return ""
}

// Turn counts one assistant turn.
func (m *Meter) Turn() End {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++
	if m.caps.MaxTurns > 0 && m.turns > m.caps.MaxTurns && m.trip(EndTurns) {
		return EndTurns
	}
	return ""
}

// Tripped returns the first cap hit, or "".
func (m *Meter) Tripped() End {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.end
}

// Cost returns the cost charged so far.
func (m *Meter) Cost() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cost
}
