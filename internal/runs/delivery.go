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

// PluginSkillSource is what the run registry needs from the plugin system
// (ADR-0042): the text of an active plugin's skill for a profile that names
// it, and the read-only delivery of every active plugin skill to a run.
// Both follow the plugin state at call time, so a skill of a disabled plugin
// is neither inlined nor delivered.
type PluginSkillSource interface {
	PluginSkillBody(qualified string) (string, bool)
	PluginSkillDelivery(provider taskrunner.Provider) taskrunner.Delivery
}

// SetPluginSkills installs the plugin skill source (nil removes it).
func (reg *Registry) SetPluginSkills(src PluginSkillSource) {
	reg.observersMu.Lock()
	reg.pluginSkills = src
	reg.observersMu.Unlock()
}

func (reg *Registry) pluginSkillSource() PluginSkillSource {
	reg.observersMu.Lock()
	defer reg.observersMu.Unlock()
	return reg.pluginSkills
}

// mergeDelivery adds the plugin skills' delivery to the learned-skill one.
func mergeDelivery(a, b taskrunner.Delivery) taskrunner.Delivery {
	a.AddDirs = append(append([]string(nil), a.AddDirs...), b.AddDirs...)
	if b.Preface != "" {
		if a.Preface != "" {
			a.Preface += "\n"
		}
		a.Preface += b.Preface
	}
	return a
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
	ps := reg.pluginSkills
	reg.observersMu.Unlock()
	if r.purpose != "" {
		return ctx
	}
	var pd taskrunner.Delivery
	if ps != nil {
		func() {
			defer func() {
				if v := recover(); v != nil {
					slog.Error("runs: plugin skill delivery panicked; run proceeds without it", "run", r.id, "panic", v)
					pd = taskrunner.Delivery{}
				}
			}()
			pd = ps.PluginSkillDelivery(r.provider)
		}()
	}
	if p == nil {
		if pd.Empty() {
			return ctx
		}
		return taskrunner.WithDelivery(ctx, pd)
	}
	task, err := reg.st.GetTask(r.taskID)
	if err != nil {
		slog.Warn("runs: learned-skill delivery skipped", "run", r.id, "err", err)
		if pd.Empty() {
			return ctx
		}
		return taskrunner.WithDelivery(ctx, pd)
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
	if !d.Empty() {
		r.mu.Lock()
		r.offered = oi
		r.mu.Unlock()
	}
	d = mergeDelivery(d, pd)
	if d.Empty() {
		return ctx
	}
	return taskrunner.WithDelivery(ctx, d)
}
