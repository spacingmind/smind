package probesvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// realCanary runs the sandbox self-check against the real CLI under
// <Home>/probe/launch/<probeID>-canary, removing everything it created. The
// cost is returned even when the check fails.
func (s *Service) realCanary(ctx context.Context, probeID int64) (sandbox.Attestation, float64, error) {
	if s.d.RealCLI == nil {
		return sandbox.Attestation{}, 0, fmt.Errorf("no claude CLI resolver")
	}
	cli, err := s.d.RealCLI()
	if err != nil {
		return sandbox.Attestation{}, 0, fmt.Errorf("resolve claude: %w", err)
	}
	if err := os.MkdirAll(s.launchRoot(), 0o700); err != nil {
		return sandbox.Attestation{}, 0, err
	}
	dir := filepath.Join(s.launchRoot(), fmt.Sprintf("%d-canary", probeID))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return sandbox.Attestation{}, 0, err
	}
	defer removeAll(dir)
	priv := filepath.Join(dir, "private")
	if err := os.Mkdir(priv, 0o700); err != nil {
		return sandbox.Attestation{}, 0, err
	}
	// The canary's arm and outside dirs live in their own 0700 parent so the
	// private dir (launcher, pidfile) is not nested in either.
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return sandbox.Attestation{}, 0, err
	}
	c, err := sandbox.NewCanary(home)
	if err != nil {
		return sandbox.Attestation{}, 0, err
	}
	l, err := sandbox.Prepare(sandbox.Config{ArmDir: c.ArmDir, ProjectionDir: c.ProjectionDir, PrivateDir: priv, RealCLI: cli})
	if err != nil {
		return sandbox.Attestation{}, 0, err
	}
	att, obs, err := sandbox.RunCanary(ctx, l, c, s.model(), s.d.Now)
	return att, obs.CostUSD, err
}
