package quota

import (
	"sync"
	"time"
)

const (
	// resumeGap is how much longer the wall clock must have run than the
	// monotonic clock between two observations to count as a resume.
	resumeGap = 60 * time.Second
	// resumeWindow is how long fetches stay spread after a resume.
	resumeWindow = 2 * time.Minute
	// wakeSpreadMax is the longest random wait a non-forced fetch takes
	// inside the resume window.
	wakeSpreadMax = 30 * time.Second
)

// resumeWatch detects that the machine slept. A sleeping machine stops the
// monotonic clock but not the wall clock, so the difference between how far
// the wall clock moved and how far the monotonic clock moved between two
// observations is the time spent asleep. A plain idle moves both clocks
// equally and is never a resume; a small difference is clock skew.
type resumeWatch struct {
	wall func() time.Time
	mono func() time.Duration

	mu       sync.Mutex
	seen     bool
	prevWall time.Time
	prevMono time.Duration
	until    time.Time
}

// newResumeWatch returns a watch on the real clocks: time.Now for the wall
// and time elapsed since creation (monotonic) for the mono clock. nil
// arguments select those defaults.
func newResumeWatch(wall func() time.Time, mono func() time.Duration) *resumeWatch {
	if wall == nil {
		wall = time.Now
	}
	if mono == nil {
		start := time.Now()
		mono = func() time.Duration { return time.Since(start) }
	}
	return &resumeWatch{wall: wall, mono: mono}
}

// observe records both clocks and reports whether a resume happened since
// the previous call. The first call is never a resume.
func (w *resumeWatch) observe() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	wallNow, monoNow := w.wall(), w.mono()
	resumed := w.seen && (wallNow.Sub(w.prevWall))-(monoNow-w.prevMono) > resumeGap
	w.seen, w.prevWall, w.prevMono = true, wallNow, monoNow
	if resumed {
		w.until = wallNow.Add(resumeWindow)
	}
	return resumed
}

// active reports whether now is inside the post-resume window.
func (w *resumeWatch) active(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return now.Before(w.until)
}
