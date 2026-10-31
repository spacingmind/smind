package wsapi

// daemon_info.go -- the `daemon.info` method (ADR-0031 P10): what a client
// needs to tell whether the daemon it is connected to is older than itself.
// It rides the existing /ws connection, so it also works over a relay,
// where /healthz is unreachable. It carries no secrets and is deliberately
// not localOnly: a paired device may read it.

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/version"
)

// DaemonInfo is the `daemon.info` result.
type DaemonInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	// ManagedBy names the OS service manager that supervises this daemon
	// ("systemd", "launchd", "taskscheduler"); empty when none does. Same value as
	// /healthz `managedBy`.
	ManagedBy string `json:"managedBy,omitempty"`
	// Platform is GOOS/GOARCH, e.g. "linux/amd64".
	Platform  string `json:"platform"`
	StartedAt string `json:"startedAt"` // RFC 3339, UTC
	// Forge is the forge CLI capability of the daemon host (ADR-0039 §8, which
	// ADR-0038 §8 defers to): `cli` is a PATH lookup, never a process spawn.
	// An object so authentication and host details can join it.
	Forge *ForgeInfo `json:"forge,omitempty"`
	// Attachments are the prompt-attachment limits of this connection;
	// absent on a daemon without an attachment store (ADR-0038).
	Attachments *AttachmentsInfo `json:"attachments,omitempty"`
	// Files are the binary file-read limits of this connection (ADR-0049 §2.4);
	// absent on a daemon that predates file.stat / file.readRange.
	Files *FilesInfo `json:"files,omitempty"`
}

// ForgeInfo is daemon.info's `forge` field.
type ForgeInfo struct {
	// Provider is the forge this daemon talks to; "github" is the only one.
	Provider string `json:"provider"`
	// CLI reports whether the provider's CLI (gh) is on the daemon's PATH.
	CLI bool `json:"cli"`
}

// forgeCLIInstalled is a seam so tests do not depend on the host's PATH.
var forgeCLIInstalled = func() bool {
	_, err := exec.LookPath("gh")
	return err == nil
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

func handleDaemonInfo(info *DaemonInfo, reg *runs.Registry) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		out := *info
		out.Forge = &ForgeInfo{Provider: "github", CLI: forgeCLIInstalled()}
		out.Files = filesInfo(connMaxMessage(rc))
		if reg != nil && reg.AttachmentStore() != nil {
			out.Attachments = attachmentsInfo(connMaxMessage(rc))
		}
		return out, nil
	}
}
