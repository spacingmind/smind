package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
)

// stopWaitTimeout bounds how long `smind stop` waits for the daemon to
// actually go down after accepting the shutdown request: graceful
// shutdown gives in-flight work 5s (cmdServe's httpSrv.Shutdown timeout)
// plus run/terminal teardown, so 30s is generous headroom without
// hanging forever on a wedged daemon.
const stopWaitTimeout = 30 * time.Second

// stopPollInterval is the cadence of the "is the port still answering?"
// poll below -- fast enough to feel instant, slow enough not to spin.
const stopPollInterval = 250 * time.Millisecond

// cmdStop asks the running daemon to shut down gracefully via its
// authenticated POST /api/shutdown endpoint (ADR-0029 D2), using the port
// from config and the token from the same location the daemon itself
// wrote (dialDaemon's credential model), then waits until the daemon
// stops answering /healthz. It is the mechanism every caller can reach
// -- CLI, tests, a future native Windows desktop manager -- because it
// needs only the daemon's port and token, not a shared console.
func cmdStop(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind stop")
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "smind stop: config: %v\n", err)
		return 1
	}
	token, err := auth.LoadOrCreateToken(config.Dir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "smind stop: auth: %v\n", err)
		return 1
	}

	port := cfg.Server.Port
	if port == 0 {
		port = 4648
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	base := "http://" + addr

	if code := requestShutdown(base, token); code != 0 {
		return code
	}

	deadline := time.Now().Add(stopWaitTimeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			// Connection refused/failed = nothing answering the port
			// anymore: the daemon is down.
			fmt.Printf("smind stopped\n")
			return 0
		}
		_ = resp.Body.Close()
		cancel()
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "smind stop: daemon at %s still answering after %s\n", addr, stopWaitTimeout)
			return 1
		}
		time.Sleep(stopPollInterval)
	}
}

// requestShutdown sends the authenticated POST /api/shutdown and maps
// every failure mode to an exit code (after printing why): unreachable
// daemon, rejected token, unexpected status.
func requestShutdown(base, token string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/shutdown", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smind stop: %v\n", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		hint := "is `smind serve` running?"
		if errors.Is(err, context.DeadlineExceeded) {
			hint = "daemon accepted the connection but never answered"
		}
		fmt.Fprintf(os.Stderr, "smind stop: request to %s failed (%s): %v\n", base, hint, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusAccepted:
		return 0
	case http.StatusUnauthorized:
		fmt.Fprintf(os.Stderr, "smind stop: token rejected by daemon at %s (token mismatch: is SMIND_HOME pointing at the daemon's home?)\n", base)
		return 1
	default:
		fmt.Fprintf(os.Stderr, "smind stop: daemon at %s answered %s\n", base, resp.Status)
		return 1
	}
}
