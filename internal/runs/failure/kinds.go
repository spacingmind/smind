package failure

// IsPlanLimit reports whether kind is a limit on the plan: the plan window or
// the residual unclassifiable rate limit. These are the kinds the provider
// cooldown, the `wait` / `fallback` profile policy and the stall detector act
// on (ADR-0041 F1/F2b).
func IsPlanLimit(kind string) bool {
	return kind == KindPlanWindowExhausted || kind == KindRateLimited
}

// IsLimitKind reports whether kind is any rate-limit class: a plan limit or a
// transient 429. Consumers that only want "was this a limit" use this, never
// a literal compare (AC15).
func IsLimitKind(kind string) bool {
	return IsPlanLimit(kind) || kind == KindTransient429
}

// IsHumanFix reports whether kind needs a person to change setup (sign in, or
// pick another model): never retried, never failed over, never on cooldown.
func IsHumanFix(kind string) bool {
	return kind == KindAuthExpired || kind == KindModelNotInPlan
}

// IsKnown reports whether kind is one of the kinds this daemon produces.
func IsKnown(kind string) bool {
	return IsLimitKind(kind) || IsHumanFix(kind) || kind == KindCLIExited
}
