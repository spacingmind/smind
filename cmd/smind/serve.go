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
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/server"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// noopQuotaFetcher always reports zero usage. Real per-provider usage
// polling (Anthropic/OpenAI/etc. quota APIs) isn't implemented yet, so this
// is a known gap, not a mistake: routing.Router treats an account with no
// known limit as available, so this keeps routing failing open (never
// blocking on quota) until real quota fetching lands.
type noopQuotaFetcher struct{}

func (noopQuotaFetcher) Fetch(ctx context.Context, account store.Account) (quota.Usage, error) {
	return quota.Usage{}, nil
}

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
	poller := quota.New(db, noopQuotaFetcher{})
	router := routing.New(db, registry, poller)
	wm := workspace.New(db)

	var runnerOpts []taskrunner.Option
	if cmd := os.Getenv("SMIND_ACP_COMMAND"); cmd != "" {
		// Testing-only hook: lets a manual/integration test point
		// ProviderGLM turns at a fake ACP agent binary (see
		// internal/taskrunner/fakeagent) instead of the real, npx-installed
		// GLM agent, so the daemon can be smoke-tested end-to-end without
		// real GLM credentials or network access. Unset (the normal case),
		// this changes nothing: taskrunner.New's own default
		// (acp.GLMCommand()) applies exactly as before.
		runnerOpts = append(runnerOpts, taskrunner.WithACPCommand(strings.Fields(cmd)))
	}
	runner := taskrunner.New(wm, runnerOpts...)

	token, err := auth.LoadOrCreateToken(config.Dir())
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	log.Printf("auth token: %s", auth.TokenPath(config.Dir()))

	srv := server.New(cfg, registry, router, wm, runner, token)
	httpSrv := &http.Server{
		Addr:              srv.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("smind listening on http://%s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Printf("smind stopped")
	return 0
}
