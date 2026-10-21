package probesvc

import (
	"context"

	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// running is the in-flight probe.
type running struct {
	probeID   int64
	proposal  int64
	cancel    context.CancelFunc
	cancelled bool
	runID     string // the arm run currently executing, "" between arms
}

// canaryFunc runs one sandbox self-check and returns the attestation and the
// cost it incurred (also on failure).
type canaryFunc func(ctx context.Context, probeID int64) (sandbox.Attestation, float64, error)

// armFunc runs one arm (see arm.go).
type armFunc func(ctx context.Context, job armJob) (armOutcome, error)

func (s *Service) initCtx() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

// Close cancels the running probe and waits for its goroutine.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}
