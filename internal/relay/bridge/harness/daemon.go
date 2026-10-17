package main

import (
	"context"
	"sync"

	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/e2ee"
)

// daemon is the restartable daemon side in hub mode: one registry and one
// keypair for life, a new Hub (so new, empty E2EE session state) per start.
type daemon struct {
	opts bridge.Options
	cfg  bridge.Config
	kp   *e2ee.KeyPair
	api  bridge.Server

	mu     sync.Mutex
	hub    *bridge.Hub
	cancel context.CancelFunc
	done   chan struct{}
}

func newDaemon(opts bridge.Options, cfg bridge.Config, kp *e2ee.KeyPair, api bridge.Server) *daemon {
	return &daemon{opts: opts, cfg: cfg, kp: kp, api: api}
}

// current is the live hub (nil while restarting).
func (d *daemon) current() *bridge.Hub {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hub
}

func (d *daemon) start(parent context.Context) {
	hub := bridge.NewHub(d.opts)
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	d.mu.Lock()
	d.hub, d.cancel, d.done = hub, cancel, done
	d.mu.Unlock()
	go func() {
		defer close(done)
		_ = hub.Run(ctx, d.cfg, d.kp, d.api)
	}()
}

func (d *daemon) stop() {
	d.mu.Lock()
	cancel, done := d.cancel, d.done
	d.hub, d.cancel, d.done = nil, nil, nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// restart drops the hub and starts a fresh one on the same registry and key.
func (d *daemon) restart(parent context.Context) {
	d.stop()
	d.start(parent)
}
