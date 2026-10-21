// Package verdict turns a probe's contrasts into the advisory verdict of
// ADR-0037 §4 (D9). It is pure: callers pass the contrasts and arm ends.
package verdict

import "github.com/spacingmind/smind/internal/learning/probe/contrast"

// Code is the verdict.
type Code string

// Verdict codes.
const (
	Helped       Code = "helped"
	NoEffect     Code = "no_effect"
	NeverLoaded  Code = "never_loaded"
	Hurt         Code = "hurt"
	Inconclusive Code = "inconclusive"
	ProbeFailed  Code = "probe_failed"
	Unsafe       Code = "unsafe"
)

// ArmEnd is how one arm's run ended.
type ArmEnd string

// Arm ends.
const (
	EndDone    ArmEnd = "done"
	EndCapped  ArmEnd = "capped"
	EndTimeout ArmEnd = "timeout"
	EndTurns   ArmEnd = "turns"
	EndOutput  ArmEnd = "output"
	EndKilled  ArmEnd = "killed"
	EndError   ArmEnd = "error"
)

// Input is everything the verdict is a function of.
type Input struct {
	Quick        bool
	HasSignature bool // the evidence kind has a failure signature
	Contrast     contrast.Contrast
	// Ends holds the end status of every arm that ran; a missing planned
	// arm means it never ran.
	Ends    map[contrast.Role]ArmEnd
	Planned []contrast.Role
	Unsafe  bool // a tripwire fired
}

// Result is the advisory verdict and its one-sentence reason.
type Result struct {
	Code   Code
	Reason string
}

func recurred(c contrast.Contrast, role contrast.Role) bool {
	v, ok := c.Recurred[role]
	return ok && v != nil && *v
}

func rowWorse(c contrast.Contrast, key contrast.MetricKey) bool {
	for _, r := range c.Metrics {
		if r.Key == key {
			return r.Worse
		}
	}
	return false
}

func anyRowWorse(c contrast.Contrast) bool {
	for _, r := range c.Metrics {
		if r.Key != contrast.KeyDiffBytes && r.Worse {
			return true
		}
	}
	return false
}

// Decide applies the precedence of ADR-0037 §4, first match wins.
func Decide(in Input) Result {
	// 1. a tripwire fired.
	if in.Unsafe {
		return Result{Unsafe, "a tripwire fired and the probe was quarantined."}
	}
	// 2. a planned arm never ran, or ended in error or killed.
	for _, r := range in.Planned {
		end, ran := in.Ends[r]
		if !ran || end == EndError || end == EndKilled {
			return Result{ProbeFailed, "an arm failed to run to completion, so there is no readable contrast."}
		}
	}
	// 3. an arm hit its limit.
	for _, r := range in.Planned {
		switch in.Ends[r] {
		case EndCapped, EndTimeout, EndTurns, EndOutput:
			return Result{Inconclusive, "an arm hit its limit, so the runs are not comparable."}
		}
	}
	// 4. the candidate was never loaded (also under --quick).
	if !in.Contrast.Loaded {
		return Result{NeverLoaded, "the skill was never loaded in the replay: the trigger cue did not fire."}
	}
	// 5. quick has no noise floor.
	if in.Quick {
		return Result{Inconclusive, "the quick probe has no noise floor."}
	}
	// 6. hurt: worse failures or denials, a worse probeCheck, or the
	// failure appearing only with the skill.
	if rowWorse(in.Contrast, contrast.KeyFailedToolCalls) ||
		rowWorse(in.Contrast, contrast.KeyPermissionDenials) ||
		in.Contrast.CheckWorse {
		return Result{Hurt, "the skill made the replay worse on a measured count."}
	}
	if in.HasSignature && !recurred(in.Contrast, contrast.RoleA) && recurred(in.Contrast, contrast.RoleB) {
		return Result{Hurt, "the failure appeared with the skill but not in the control."}
	}
	// 7. helped: failure in both controls, gone with the skill, nothing
	// worse anywhere.
	if in.HasSignature &&
		recurred(in.Contrast, contrast.RoleA) && recurred(in.Contrast, contrast.RoleA2) &&
		!recurred(in.Contrast, contrast.RoleB) &&
		!anyRowWorse(in.Contrast) && !in.Contrast.CheckWorse {
		return Result{Helped, "the skill fixes the observed failure here."}
	}
	// 8. no effect: loaded, nothing worse, and the failure state matches
	// the control (or there is no failure signature to compare).
	if !anyRowWorse(in.Contrast) &&
		((in.HasSignature && recurred(in.Contrast, contrast.RoleA) && recurred(in.Contrast, contrast.RoleB)) || !in.HasSignature) {
		return Result{NoEffect, "the replay showed no observed difference beyond the noise floor."}
	}
	// 9. otherwise inconclusive, with the specific reason.
	if in.HasSignature {
		if !recurred(in.Contrast, contrast.RoleA) {
			return Result{Inconclusive, "the failure did not reproduce in the control."}
		}
		if _, ok := in.Contrast.Recurred[contrast.RoleA2]; ok && !recurred(in.Contrast, contrast.RoleA2) {
			return Result{Inconclusive, "the controls disagree."}
		}
	}
	return Result{Inconclusive, "B was worse than the noise floor on cost or time."}
}
