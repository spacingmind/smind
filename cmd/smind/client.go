package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/wsclient"
)

// dialTimeout bounds dialDaemon's dial (TCP connect + WS handshake). The
// daemon is local, so a healthy dial completes in milliseconds; without a
// bound, an unreachable address (a port nothing answers on -- which on
// some networks black-holes SYNs instead of refusing them) hangs the CLI
// for the OS-level connect timeout before the "is smind serve running?"
// message ever appears.
const dialTimeout = 5 * time.Second

// dialDaemon connects to the locally running smind daemon's /ws endpoint,
// reading the port from config and the auth token the same way the daemon
// itself does (config.Load + auth.LoadOrCreateToken) -- no separate
// CLI-side credential mechanism. Remote daemons (--host or equivalent)
// are out of scope: smind is single-machine for now.
func dialDaemon(ctx context.Context) (*wsclient.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	token, err := auth.LoadOrCreateToken(config.Dir())
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	port := cfg.Server.Port
	if port == 0 {
		port = 4648
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	client, err := wsclient.Dial(ctx, addr, token)
	if err != nil {
		return nil, fmt.Errorf("connect to smind daemon at %s (is `smind serve` running?): %w", addr, err)
	}
	return client, nil
}
