package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/logging"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/devices"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/service"
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
		// Launch flags were removed (ADR-0031 D10): one documented place
		// configures the daemon, the same for a terminal, a service and Docker.
		for _, a := range args {
			if strings.HasPrefix(a, "--port") || strings.HasPrefix(a, "--listen") || strings.HasPrefix(a, "--host") || strings.HasPrefix(a, "--bind") || strings.HasPrefix(a, "--password") {
				fmt.Fprintf(os.Stderr, "smind serve: %s was removed; use `smind config set server.listen <host:port>` (or server.port, server.hostnames), `smind set-password`, then restart\n", strings.SplitN(a, "=", 2)[0])
				return 2
			}
		}
		fmt.Fprintln(os.Stderr, "usage: smind serve")
		return 2
	}

	// The unit/plist `smind service install` writes sets SMIND_SUPERVISOR so
	// /healthz can say an OS service manager owns this process (ADR-0031
	// decision 6). Take it out of the environment: agents and shells this
	// daemon spawns inherit it otherwise, and a `smind serve` started by one of
	// them would wrongly claim to be service-managed too.
	supervisor := takeSupervisorEnv()

	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v (continuing with defaults)", err)
	}

	// The password is in cfg now. Take it out of the environment, like
	// SMIND_SUPERVISOR above: every shell and agent this daemon spawns would
	// inherit it, and `env` in an agent would put the login credential into a
	// transcript and the stored run logs.
	_ = os.Unsetenv(config.EnvPassword)

	closeLog := setupDaemonLog(cfg.Log, os.Stderr)
	defer closeLog()

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
	// ADR-0039: one-time, best-effort fill of base_sha/base_ref/branch_owned
	// for tasks that predate them (idempotent; unrecoverable ones stay empty).
	if n := wm.BackfillTaskBases(); n > 0 {
		log.Printf("backfilled the base commit of %d task(s)", n)
	}
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
	for _, id := range config.DefaultModelProviders {
		if m := cfg.DefaultModel(id); m != "" {
			runnerOpts = append(runnerOpts, taskrunner.WithDefaultModel(taskrunner.Provider(id), m))
		}
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

	srv, err := server.New(cfg, registry, router, wm, runner, db, token, server.WithQuotaObserver(passive), server.WithManagedBy(supervisor))
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	srv.API().AttachUsage(poller, registry, passive)
	srv.API().Runs.SetMaxConcurrentRunsPerTree(cfg.Orchestration.MaxConcurrentRunsPerTree)
	wireAttachments(srv.API(), wm, db)
	// Push and fetch to an https origin use the forge login (ADR-0039 O9),
	// per invocation and only while it is signed in.
	workspace.SetNetworkGitConfig(srv.API().NetworkGitConfig())
	// The relay device admin methods must be registered before the first
	// connection is served (SetRelayDevices is not safe against concurrent
	// requests).
	hub := newRelayHub(srv, cfg.Relay)
	// Same startup-only rule for the push.* methods (ADR-0040).
	pushRT := wirePush(srv.API(), hub, db, cfg.Push)
	httpSrv := &http.Server{
		Addr:              srv.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Bind every address before announcing anything, so a port conflict
	// fails the start instead of leaving a half-listening daemon.
	var listeners []net.Listener
	for _, b := range srv.Binds() {
		ln, err := net.Listen(b.Network, b.Addr)
		if err != nil {
			log.Fatalf("listen: %v", err)
		}
		listeners = append(listeners, ln)
	}
	if !cfg.Server.ListenIsLoopback() {
		var bound []string
		for _, ln := range listeners {
			bound = append(bound, ln.Addr().String())
		}
		log.Printf("WARNING: listening beyond this machine on %s: login is by password over plain HTTP (anyone on the network path can read it); use a trusted network, Tailscale, an SSH tunnel or a TLS-terminating proxy", strings.Join(bound, ", "))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ADR-0039 §4: the git state loop idles until a client subscribes to task.git.
	stopGitState := srv.API().StartGitState(ctx)

	stopLearning := wireLearning(ctx, learningWiring{
		Cfg: cfg, DB: db, API: srv.API(), WM: wm, Runner: runner, Reg: srv.API().Runs,
		Home: config.Dir(), Secrets: []string{token},
	})

	for _, ln := range listeners {
		go func() {
			log.Printf("smind listening on http://%s", ln.Addr())
			if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("listen: %v", err)
			}
		}()
	}

	startRelayBridge(ctx, srv, hub)

	// One shutdown path for both triggers: the signal context (SIGINT/
	// SIGTERM) and the authenticated POST /api/shutdown, whose channel
	// the server closes (see Server.ShutdownSignal). Whichever fires first,
	// the identical graceful-shutdown sequence below runs.
	select {
	case <-ctx.Done():
	case <-srv.ShutdownSignal():
	}

	stopLearning() // before the run registry closes: in-flight jobs end failed, watermarks untouched
	stopGitState()
	pushRT.Close() // no push goroutine may outlive the daemon
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

// takeSupervisorEnv reads SMIND_SUPERVISOR and removes it from the process
// environment, so the agents and shells this daemon spawns do not inherit it.
func takeSupervisorEnv() string {
	v := strings.TrimSpace(os.Getenv(service.SupervisorEnv))
	_ = os.Unsetenv(service.SupervisorEnv)
	return v
}

// setupDaemonLog points the standard logger at stderr *and* the daemon's own
// rotating log, $SMIND_HOME/daemon.log, so `smind service logs` reads the same
// thing on every OS and no unit has to redirect output (ADR-0031 decision 7).
// The file is best effort: if it cannot be opened the daemon logs to stderr
// only, and a write error never reaches the stderr copy. The returned func
// closes the file.
func setupDaemonLog(lc config.LogConfig, stderr io.Writer) func() {
	dir := config.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("log file: %v (logging to stderr only)", err)
		return func() {}
	}
	path := filepath.Join(dir, logging.FileName)
	rf, err := logging.OpenRotating(path, int64(lc.MaxSizeMB)<<20, lc.MaxFiles)
	if err != nil {
		log.Printf("log file %s: %v (logging to stderr only)", path, err)
		return func() {}
	}
	// Both best effort: io.MultiWriter stops at the first failing writer, and a
	// closed terminal or pipe on stderr must not silence the file (or the other
	// way round).
	log.SetOutput(io.MultiWriter(logging.BestEffort{W: stderr}, logging.BestEffort{W: rf}))
	// Panic traces must land in daemon.log too (under systemd that is the
	// journal's job, under launchd/the Windows task nobody reads stderr).
	// Best effort: on failure the daemon continues with stderr-only traces.
	co, err := logging.InstallCrashOutput(path)
	if err != nil {
		log.Printf("crash output: %v (panic traces go to stderr only)", err)
	} else {
		rf.OnRotate(func() { _ = co.Reopen() })
	}
	// The crash output is NOT unset here: deferred funcs run during panic
	// unwinding, before the runtime prints the trace — unsetting it would
	// lose the trace we installed it for. The process exits right after.
	return func() { _ = rf.Close() }
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
	if rc.LegacyDevices {
		log.Printf("relay: WARNING: relay.legacyDevices is true — a relay client without device identity is served WITHOUT approval and cannot be revoked (anyone with the relay host secret and this daemon's public key can connect). Update the desktop/mobile apps and remove the setting.")
	}
	hub := bridge.NewHub(bridge.Options{Registry: reg, LegacyDevices: rc.LegacyDevices, MaxDevices: rc.MaxDevices, Password: srv.RelayPasswordGate()})
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
