package quota

import (
	"testing"
	"time"
)

func Test_resumewatch_detects_wall_gap_not_idle(t *testing.T) {
	wall := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var mono time.Duration
	w := newResumeWatch(func() time.Time { return wall }, func() time.Duration { return mono })

	if w.observe() {
		t.Fatal("first observation must never be a resume")
	}
	// Long idle: both clocks advance 10 minutes.
	wall, mono = wall.Add(10*time.Minute), mono+10*time.Minute
	if w.observe() {
		t.Fatal("a long idle is not a resume")
	}
	// Skew of 5 s.
	wall, mono = wall.Add(6*time.Second), mono+time.Second
	if w.observe() {
		t.Fatal("a small skew is not a resume")
	}
	if w.active(wall) {
		t.Fatal("window must not be active without a resume")
	}
	// Sleep: wall +10 min, mono +1 s.
	wall, mono = wall.Add(10*time.Minute), mono+time.Second
	if !w.observe() {
		t.Fatal("a wall step far beyond the mono step is a resume")
	}
	if !w.active(wall) || !w.active(wall.Add(resumeWindow-time.Second)) {
		t.Fatal("window must be active for 2 minutes")
	}
	if w.active(wall.Add(resumeWindow)) {
		t.Fatal("window must end after 2 minutes")
	}
}
