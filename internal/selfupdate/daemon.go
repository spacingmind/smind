package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// DaemonStatus is what an update needs to know about the local daemon.
type DaemonStatus struct {
	Running bool
	Version string
	// Managed is true when an OS service manager supervises the daemon
	// (healthz `managedBy`, set from SMIND_SUPERVISOR by `smind service`,
	// ADR-0031 D2/decision 6). Only then may an update restart it.
	Managed bool
}

// Daemon is the running daemon as seen by the updater.
type Daemon interface {
	// Status probes the daemon's /healthz. A daemon that is not answering is
	// (DaemonStatus{}, nil), not an error.
	Status(ctx context.Context) (DaemonStatus, error)
	// Restart restarts the service-managed daemon using exe, the binary that
	// is installed at the moment of the call.
	Restart(ctx context.Context, exe string) error
}

// Activity is the work an update would interrupt.
type Activity struct {
	// DaemonRunning is whether a daemon answered at all.
	DaemonRunning bool
	// RunningRuns counts runs in the running state. A run waiting on a
	// permission prompt is still a running run, so this covers pending
	// permissions too.
	RunningRuns int
}

// HealthzDaemon implements Daemon over the daemon's unauthenticated /healthz.
type HealthzDaemon struct {
	BaseURL string // e.g. http://127.0.0.1:4648
	Client  *http.Client
}

type healthz struct {
	Version   string `json:"version"`
	ManagedBy string `json:"managedBy"`
}

func (d *HealthzDaemon) Status(ctx context.Context) (DaemonStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.BaseURL+"/healthz", nil)
	if err != nil {
		return DaemonStatus{}, err
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return DaemonStatus{}, err
		}
		return DaemonStatus{}, nil // nothing answering the port
	}
	defer func() { _ = resp.Body.Close() }()
	var h healthz
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&h) != nil {
		// Something owns the port but is not a smind daemon we understand;
		// treat it as running-and-unmanaged so we never restart it.
		return DaemonStatus{Running: true}, nil
	}
	return DaemonStatus{Running: true, Version: h.Version, Managed: h.ManagedBy != ""}, nil
}

// Restart runs `<exe> service restart`. The freshly installed binary is used
// (not the process doing the update) because only it is guaranteed to know the
// service definition of the version being installed.
func (d *HealthzDaemon) Restart(ctx context.Context, exe string) error {
	out, err := exec.CommandContext(ctx, exe, "service", "restart").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s service restart: %w: %s", exe, err, strings.TrimSpace(string(out)))
	}
	return nil
}
