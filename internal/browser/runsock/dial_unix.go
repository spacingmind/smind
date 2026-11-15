//go:build !windows

package runsock

import (
	"context"
	"net"
)

// Dial connects to a run's browser socket (the child's side).
func Dial(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
