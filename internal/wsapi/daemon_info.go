package wsapi

// daemon_info.go -- the `daemon.info` method (ADR-0031 P10): what a client
// needs to tell whether the daemon it is connected to is older than itself.
// It rides the existing /ws connection, so it also works over a relay,
// where /healthz is unreachable. It carries no secrets and is deliberately
// not localOnly: a paired device may read it.

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"github.com/spacingmind/smind/internal/version"
)

// DaemonInfo is the `daemon.info` result.
type DaemonInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	// ManagedBy names the OS service manager that supervises this daemon
	// ("systemd", "launchd", "task"); empty when none does. Same value as
	// /healthz `managedBy`.
	ManagedBy string `json:"managedBy,omitempty"`
	// Platform is GOOS/GOARCH, e.g. "linux/amd64".
	Platform  string `json:"platform"`
	StartedAt string `json:"startedAt"` // RFC 3339, UTC
}

// newDaemonInfo describes this process, started at start.
func newDaemonInfo(start time.Time) *DaemonInfo {
	return &DaemonInfo{
		Version:   version.Version,
		Commit:    version.Commit,
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		StartedAt: start.UTC().Format(time.RFC3339),
	}
}

// SetManagedBy records the supervisor reported by `daemon.info`. Like
// SetRelayDevices it must be called at startup, before the API serves any
// connection (it writes without locking).
func (a *API) SetManagedBy(supervisor string) {
	a.info.ManagedBy = supervisor
}

func handleDaemonInfo(info *DaemonInfo) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		return *info, nil
	}
}
