// Command smind runs the Spacing Mind daemon.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
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

func main() {
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
	runner := taskrunner.New(wm)

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
}
