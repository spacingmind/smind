// Package lifecycle holds the pure skill-lifecycle decision function for
// ADR-0026's curator review: which active learned skills to archive
// (zero-load graduation and capacity eviction). No store, no I/O, no clock
// -- callers feed in the snapshot and apply the returned decisions.
package lifecycle

import (
	"crypto/sha256"
	"sort"
	"strconv"
)

// Skill is the curator's read-model slice of a learned skill.
type Skill struct {
	ID          int64
	Name        string
	Status      string
	Pinned      bool
	Measured    bool
	OfferedRuns int
	LoadedRuns  int
}

// Params tunes the review. Zero fields are not special-cased: the caller
// passes explicit values (the plan's defaults are 25 and 30).
type Params struct {
	ProbationRuns int
	Capacity      int
}

// Action is what the caller should do with a skill. Only "archive" exists
// today; the type leaves room for future actions without breaking callers.
type Action string

// ActionArchive retires the skill (status archived).
const ActionArchive Action = "archive"

// Reasons for archive decisions.
const (
	ReasonGraduationZeroLoad = "graduation_zero_load"
	ReasonCapacity           = "capacity"
)

// Decision is one review outcome for one skill.
type Decision struct {
	ID     int64
	Action Action
	Reason string
}

// Evaluate decides which active skills to archive:
//
//   - Only Status == "active" skills participate; everything else is
//     ignored.
//   - Pinned skills are exempt from everything and don't count toward
//     capacity.
//   - A skill with OfferedRuns < ProbationRuns is in probation: never
//     evicted, not counted toward capacity.
//   - A measured mature skill (OfferedRuns >= ProbationRuns) with
//     LoadedRuns == 0 is archived ("graduation_zero_load"). An unmeasured
//     mature skill is never archived for zero use.
//   - Of the remaining mature active non-pinned skills, if there are more
//     than Capacity, the surplus with the lowest load rate
//     LoadedRuns/OfferedRuns is archived ("capacity"), rates compared by
//     cross-multiplication in int64 (no floats), ties broken by
//     sha256(decimal ID) bytes ascending so the output is deterministic
//     regardless of input order.
//
// The output is sorted by ID ascending. The input is not mutated.
func Evaluate(skills []Skill, p Params) []Decision {
	var candidates []Skill
	for _, k := range skills {
		if k.Status != "active" || k.Pinned {
			continue
		}
		if k.OfferedRuns < p.ProbationRuns {
			continue // probation: never evicted, not counted toward capacity
		}
		candidates = append(candidates, k)
	}

	var decisions []Decision
	var survivors []Skill
	for _, k := range candidates {
		if k.Measured && k.LoadedRuns == 0 {
			decisions = append(decisions, Decision{ID: k.ID, Action: ActionArchive, Reason: ReasonGraduationZeroLoad})
			continue
		}
		survivors = append(survivors, k)
	}

	if surplus := len(survivors) - p.Capacity; surplus > 0 {
		ordered := append([]Skill(nil), survivors...)
		sort.SliceStable(ordered, func(i, j int) bool {
			a, b := ordered[i], ordered[j]
			// a.LoadedRuns/a.OfferedRuns < b.LoadedRuns/b.OfferedRuns, in
			// int64: cross-multiply. OfferedRuns >= ProbationRuns for every
			// candidate, so a caller with ProbationRuns > 0 never divides
			// by zero.
			li := int64(a.LoadedRuns) * int64(b.OfferedRuns)
			lj := int64(b.LoadedRuns) * int64(a.OfferedRuns)
			if li != lj {
				return li < lj
			}
			// Tie: smaller sha256(decimal id) first.
			return tieBreakLess(a.ID, b.ID)
		})
		for _, k := range ordered[:surplus] {
			decisions = append(decisions, Decision{ID: k.ID, Action: ActionArchive, Reason: ReasonCapacity})
		}
	}

	sort.Slice(decisions, func(i, j int) bool { return decisions[i].ID < decisions[j].ID })
	return decisions
}

// tieBreakLess reports whether sha256(decimal a) sorts before
// sha256(decimal b) bytewise -- the deterministic tie-break so identical
// inputs in any order produce identical output.
func tieBreakLess(a, b int64) bool {
	ha := sha256.Sum256([]byte(strconv.FormatInt(a, 10)))
	hb := sha256.Sum256([]byte(strconv.FormatInt(b, 10)))
	for i := range ha {
		if ha[i] != hb[i] {
			return ha[i] < hb[i]
		}
	}
	return false
}
