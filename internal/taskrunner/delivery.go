package taskrunner

import "context"

// Delivery is the per-run learned-skill delivery (ADR-0026 section 5),
// attached to a run's ctx so RunPrompt's signature stays unchanged.
type Delivery struct {
	// AddDirs are extra read-only directories handed to the claude-native
	// CLI (--add-dir); the learned-skill projection lives there.
	AddDirs []string
	// Preface is sent as a leading prompt block on the first prompt of a
	// fresh ACP (GLM/Kimi) session. Codex ignores both fields.
	Preface string
}

// Empty reports whether d delivers nothing.
func (d Delivery) Empty() bool { return len(d.AddDirs) == 0 && d.Preface == "" }

type deliveryKey struct{}

// WithDelivery returns ctx carrying d. RunPromptIsolated never reads it:
// system runs (reflector, curator) must not see learned skills.
func WithDelivery(ctx context.Context, d Delivery) context.Context {
	return context.WithValue(ctx, deliveryKey{}, d)
}

// DeliveryFrom returns the Delivery attached to ctx, if any.
func DeliveryFrom(ctx context.Context) (Delivery, bool) {
	d, ok := ctx.Value(deliveryKey{}).(Delivery)
	return d, ok
}
