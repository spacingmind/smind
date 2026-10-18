package runs

import (
	"context"
	"log/slog"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// OfferedInfo is what a DeliveryProvider offered one run: the learned skills
// delivered to it and whether its provider can report loads (ADR-0026 s6).
type OfferedInfo struct {
	SkillIDs []int64
	Measured bool
}

// DeliveryProvider prepares the per-run learned-skill delivery (ADR-0026 s5).
// Prepare is called at most once per run, from the run's drive goroutine
// right before RunPrompt, and only for user runs (purpose ""): system runs
// never see learned skills. It must not block on slow work.
type DeliveryProvider interface {
	Prepare(ctx context.Context, workspaceID int64, runID string, provider taskrunner.Provider, purpose string) (taskrunner.Delivery, OfferedInfo)
}

// SetDeliveryProvider installs p (nil removes it).
func (reg *Registry) SetDeliveryProvider(p DeliveryProvider) {
	reg.observersMu.Lock()
	reg.deliveryProvider = p
	reg.observersMu.Unlock()
}

// withDelivery returns ctx carrying the run's Delivery and records what was
// offered on r. A panicking or failing provider means the run proceeds
// without delivery (logged once per run).
func (reg *Registry) withDelivery(ctx context.Context, r *run) context.Context {
	reg.observersMu.Lock()
	p := reg.deliveryProvider
	reg.observersMu.Unlock()
	if p == nil || r.purpose != "" {
		return ctx
	}
	task, err := reg.st.GetTask(r.taskID)
	if err != nil {
		slog.Warn("runs: learned-skill delivery skipped", "run", r.id, "err", err)
		return ctx
	}
	var (
		d  taskrunner.Delivery
		oi OfferedInfo
	)
	func() {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("runs: delivery provider panicked; run proceeds without delivery", "run", r.id, "panic", v)
				d, oi = taskrunner.Delivery{}, OfferedInfo{}
			}
		}()
		d, oi = p.Prepare(ctx, task.WorkspaceID, r.id, r.provider, r.purpose)
	}()
	if d.Empty() {
		return ctx
	}
	r.mu.Lock()
	r.offered = oi
	r.mu.Unlock()
	return taskrunner.WithDelivery(ctx, d)
}
