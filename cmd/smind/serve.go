package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// cmdServe starts the daemon: HTTP API (proxy endpoints + /ws) plus the
// embedded web UI, until SIGINT/SIGTERM, then shuts down gracefully. This
// is the previous (pre-subcommand) main()'s entire body, unchanged except
// for the SMIND_ACP_COMMAND hook below.
func cmdServe(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: smind serve")
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v (continuing with defaults)", err)
	}

	db, err := store.Open(store.DefaultPath())
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()

	registry := accounts.New(db)
	// The poller makes no request until a user surface asks (ADR-0028 §3):
	// no fetch at startup, on account events, or on a timer.
	uc := cfg.AccountUsage
	// Passive rejection cooldown (ADR-0028 §7): learns only from responses
	// the proxy already receives, so it adds no usage call. Off with either
	// accountUsage.enabled or accountUsage.passive false. A credential change
	// drops the account's cooldown.
	passive := quota.NewPassive(db, quota.WithPassiveEnabled(uc.Enabled && uc.Passive))
	defer passive.Close()
	registry.SetCredentialReplacedHook(passive.ClearCooldown)
	poller := quota.New(db, registry, quota.NewAccountFetcher(registry),
		quota.WithEnabled(uc.Enabled),
		quota.WithTTL(time.Duration(uc.TTLSeconds)*time.Second),
		quota.WithCooldowns(passive))
	defer poller.Close()
	router := routing.New(db, registry, poller,
		routing.WithUsageRouting(uc.Routing),
		routing.WithExhaustThresholdPct(uc.ExhaustThresholdPct))
	wm := workspace.New(db)
	wm.SetMaxDepth(cfg.Orchestration.MaxDepth)
	wtSettings := workspace.WorktreesSettings{
		SetupTimeout:    time.Duration(cfg.Worktrees.SetupTimeoutSeconds) * time.Second,
		TeardownTimeout: time.Duration(cfg.Worktrees.TeardownTimeoutSeconds) * time.Second,
	}
	if lo, hi, ok, err := cfg.Worktrees.Ports.PortRange(); err != nil {
		log.Fatalf("config: %v", err)
	} else if ok {
		wtSettings.HasPorts, wtSettings.PortLo, wtSettings.PortHi = true, lo, hi
	}
	wm.SetWorktreesConfig(wtSettings)

	// A second Registry over the same db is fine: it only reads.
	runnerOpts := []taskrunner.Option{taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(db)), taskrunner.WithACPModeProbe(), taskrunner.WithMcpServers(mcpservers.New(db))}
	if mt := cfg.Agents.GLM.MaxTurns; mt != nil {
		runnerOpts = append(runnerOpts, taskrunner.WithACPCommand(taskrunner.ProviderGLM, acp.GLMCommand(acp.WithMaxTurns(*mt))))
	}
	if cmd := os.Getenv("SMIND_ACP_COMMAND"); cmd != "" {
		// Testing-only hook: lets a manual/integration test point
		// ProviderGLM turns at a fake ACP agent binary (see
		// internal/taskrunner/fakeagent) instead of the real, npx-installed
		// GLM agent, so the daemon can be smoke-tested end-to-end without
		// real GLM credentials or network access. Unset (the normal case),
		// this changes nothing: taskrunner.New's own default
		// (acp.GLMCommand()) applies exactly as before.
		runnerOpts = append(runnerOpts, taskrunner.WithACPCommand(taskrunner.ProviderGLM, strings.Fields(cmd)))
	}
	runner := taskrunner.New(wm, runnerOpts...)

	token, err := auth.LoadOrCreateToken(config.Dir())
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	log.Printf("auth token: %s", auth.TokenPath(config.Dir()))

	srv, err := server.New(cfg, registry, router, wm, runner, db, token, server.WithQuotaObserver(passive))
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	srv.API().AttachUsage(poller, registry, passive)
	srv.API().Runs.SetMaxConcurrentRunsPerTree(cfg.Orchestration.MaxConcurrentRunsPerTree)
	// The relay device admin methods must be registered before the first
	// connection is served (SetRelayDevices is not safe against concurrent
	// requests).
	hub := newRelayHub(srv, cfg.Relay)
	httpSrv := &http.Server{
		Addr:              srv.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("smind listening on http://%s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	startRelayBridge(ctx, srv, hub)

	// One shutdown path for both triggers: the signal context (SIGINT/
	// SIGTERM) and the authenticated POST /api/shutdown, whose channel
	// the server closes (see Server.ShutdownSignal). Whichever fires first,
	// the identical graceful-shutdown sequence below runs.
	select {
	case <-ctx.Done():
	case <-srv.ShutdownSignal():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	wm.CancelSetups() // kill any still-running setup script's process group
	srv.Close()       // kill every still-running terminal session's shell/PTY
	log.Printf("smind stopped")
	return 0
}

// newRelayHub builds the daemon's relay device hub and wires its registry
// into the API's relay.device.* admin methods (what `smind relay devices`
// talks to). It is wired whether or not a relay is configured, so devices can
// be listed and pre-approved before the first relay connection. Call it
// before the HTTP server starts serving.
func newRelayHub(srv *server.Server, rc config.RelayConfig) *bridge.Hub {
	reg, err := devices.Open(devices.RegistryPath(config.Dir()))
	if err != nil {
		// Never reset a registry we cannot read: that would silently forget
		// revocations. Devices that identify themselves are refused until the
		// file is fixed; legacy clients are unaffected.
		log.Printf("relay devices: %v (device identity disabled until the file is fixed)", err)
		reg = nil
	}
	hub := bridge.NewHub(bridge.Options{Registry: reg, LegacyDevices: rc.LegacyDevices, MaxDevices: rc.MaxDevices})
	if reg != nil {
		srv.API().SetRelayDevices(hub)
	}
	return hub
}

// startRelayBridge runs the hub against the relay `smind relay connect`
// configured, if and only if one is persisted -- with no config present,
// nothing dials a relay and smind serve's startup/behavior are exactly as
// before this feature existed (docs/plans/active/mobile-app-milestone-1.md's
// Item 1 opt-in requirement). It runs until ctx is cancelled, alongside the
// HTTP server.
func startRelayBridge(ctx context.Context, srv *server.Server, hub *bridge.Hub) {
	dir := config.Dir()
	cfg, ok, err := bridge.LoadConfig(dir)
	if err != nil {
		log.Printf("relay bridge: %v (continuing without relay connectivity)", err)
		return
	}
	if !ok {
		return
	}
	kp, err := e2ee.LoadOrCreateKeyPair(dir)
	if err != nil {
		log.Printf("relay bridge: keypair: %v (continuing without relay connectivity)", err)
		return
	}
	go func() {
		if err := hub.Run(ctx, cfg, kp, srv.API()); err != nil {
			log.Printf("relay bridge: %v", err)
		}
	}()
}
