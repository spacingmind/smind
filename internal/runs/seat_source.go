package runs

import (
	"strings"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// stewardSeatFloor is the rule id stamped on every permission resolution the
// seat floor makes (events only; the floor never writes policy audit rows).
const stewardSeatFloor = "steward-seat-floor"

// SeatSpec is what a daemon-owned seat (the Steward review seat, ADR-0047)
// adds to its task's runs.
type SeatSpec struct {
	// Env is the extra env of the built-in smind MCP entry (review id,
	// one-time token). Policy, never logged.
	Env map[string]string
	// DisallowedTools is the provider-level deny list.
	DisallowedTools []string
	// Floor judges every permission request of the seat; it is consulted
	// BEFORE AutoAccept and the policy engine and is always on. allow=false
	// resolves to a deny.
	Floor func(tool string, paths []string) (allow bool, reason string)
}

// SetSeatSource installs the hook that supplies a daemon-owned seat's spec
// by task. The returned spec is policy, never logged.
func (reg *Registry) SetSeatSource(fn func(taskID int64) (SeatSpec, bool)) {
	reg.mu.Lock()
	reg.seatSource = fn
	reg.mu.Unlock()
}

func (reg *Registry) seatSpec(taskID int64) (SeatSpec, bool) {
	reg.mu.Lock()
	fn := reg.seatSource
	reg.mu.Unlock()
	if fn == nil {
		return SeatSpec{}, false
	}
	return fn(taskID)
}

// applySeatFloor is the seat branch of decide: it runs before AutoAccept and
// the policy engine and resolves the request itself, so nothing is parked
// and no notification goes out. handled=false (no seat, or a seat without a
// floor) leaves the request to the rest of decide. An allow the provider
// offered no allow_once option for is a deny; a deny without any reject
// option returns handled with an empty optionID and the caller errors, like
// the policy path. The floor never writes policy audit rows.
func (d runPermissionDecider) applySeatFloor(requestID, summary string, detail *taskrunner.PermissionDetail, options []taskrunner.PermissionOption) (optionID, denyReason string, handled bool) {
	spec, ok := d.reg.seatSpec(d.r.taskID)
	if !ok || spec.Floor == nil {
		return "", "", false
	}
	tool := ""
	var paths []string
	switch {
	case detail != nil && detail.Kind == "execute":
		tool = "Bash"
	case detail != nil && detail.Kind == "write":
		tool, paths = "Write", []string{detail.Path}
	default:
		if rest, ok := strings.CutPrefix(summary, "run "); ok {
			tool, _, _ = strings.Cut(rest, " ")
		}
	}
	allow, reason := spec.Floor(tool, paths)
	if allow {
		if id, ok := firstOptionByKind(options, "allow_once"); ok {
			d.recordPolicyResolved(requestID, summary, withPolicy(detail, taskrunner.PolicyInfo{RuleID: stewardSeatFloor, Why: reason, Action: "allow"}), options, id, taskrunner.PermissionResolvedByPolicyAllow, stewardSeatFloor)
			return id, "", true
		}
	}
	id, _ := firstOptionByKind(options, "reject_once", "reject_always")
	if id == "" {
		return "", "", true
	}
	d.recordPolicyResolved(requestID, summary, withPolicy(detail, taskrunner.PolicyInfo{RuleID: stewardSeatFloor, Why: reason, Action: "deny"}), options, id, taskrunner.PermissionResolvedByPolicyDeny, stewardSeatFloor)
	return id, "steward seat floor: " + reason, true
}
