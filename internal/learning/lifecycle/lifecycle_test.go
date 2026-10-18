package lifecycle

import (
	"crypto/sha256"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

func TestLifecycle_Probation(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 1, Name: "young-zero-load", Status: "active", Measured: true, OfferedRuns: 24, LoadedRuns: 0},
		{ID: 2, Name: "young-loaded", Status: "active", Measured: true, OfferedRuns: 0, LoadedRuns: 0},
	}
	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 0})
	if len(got) != 0 {
		t.Fatalf("Evaluate() = %+v, want no decisions (all in probation)", got)
	}
	// Probation skills don't count toward capacity either: a mature skill
	// alone against Capacity 0 is evicted even with probationers present.
	skills = append(skills, Skill{ID: 3, Name: "mature", Status: "active", Measured: false, OfferedRuns: 25, LoadedRuns: 5})
	got = Evaluate(skills, Params{ProbationRuns: 25, Capacity: 0})
	if len(got) != 1 || got[0].ID != 3 || got[0].Reason != ReasonCapacity {
		t.Fatalf("Evaluate() = %+v, want only skill 3 evicted for capacity", got)
	}
}

func TestLifecycle_GraduationArchivesZeroLoad(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 4, Name: "zero-load", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 0},
		{ID: 5, Name: "loaded", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 2},
	}
	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 30})
	if len(got) != 1 || got[0].ID != 4 || got[0].Action != ActionArchive || got[0].Reason != ReasonGraduationZeroLoad {
		t.Fatalf("Evaluate() = %+v, want skill 4 archived for graduation_zero_load", got)
	}
}

func TestLifecycle_UnmeasuredSkippedFromReview(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 6, Name: "unmeasured-zero", Status: "active", Measured: false, OfferedRuns: 100, LoadedRuns: 0},
		{ID: 7, Name: "measured-zero", Status: "active", Measured: true, OfferedRuns: 100, LoadedRuns: 0},
	}
	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 30})
	if len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("Evaluate() = %+v, want only the measured skill 7 archived", got)
	}
	// Unmeasured skills also escape capacity eviction... they stay in the
	// survivor pool like any other, but zero-use never archives them.
	got = Evaluate(skills, Params{ProbationRuns: 25, Capacity: 0})
	if len(got) != 2 || got[0].ID != 6 || got[1].ID != 7 {
		t.Fatalf("Evaluate() capacity 0 = %+v, want both archived", got)
	}
	if got[0].Reason != ReasonCapacity || got[1].Reason != ReasonGraduationZeroLoad {
		t.Fatalf("reasons = %+v, want capacity for 6 and graduation for 7", got)
	}
}

func TestLifecycle_CapacityEvictsLowestRate(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 11, Name: "a", Status: "active", Measured: true, OfferedRuns: 50, LoadedRuns: 10}, // .20
		{ID: 12, Name: "b", Status: "active", Measured: true, OfferedRuns: 40, LoadedRuns: 4},  // .10 lowest
		{ID: 13, Name: "c", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 9},  // .30
		{ID: 14, Name: "d", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 4},  // .13
		{ID: 15, Name: "e", Status: "active", Measured: true, OfferedRuns: 60, LoadedRuns: 30}, // .50
	}
	// Distinct rates: b .10, d .10 is NOT distinct -- adjust d so rates are
	// strictly distinct as the test name promises.
	skills[3].LoadedRuns = 3 // .15

	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 3})
	if len(got) != 2 {
		t.Fatalf("Evaluate() = %+v, want 2 evictions", got)
	}
	if got[0].ID != 12 || got[0].Reason != ReasonCapacity {
		t.Fatalf("first eviction = %+v, want skill 12 (rate .10)", got[0])
	}
	if got[1].ID != 14 || got[1].Reason != ReasonCapacity {
		t.Fatalf("second eviction = %+v, want skill 14 (rate .13)", got[1])
	}
}

func TestLifecycle_TieBreakDeterministic(t *testing.T) {
	t.Parallel()
	// Two skills with the exact same load rate, Capacity 1: one must go,
	// and it must always be the same one regardless of input order.
	skills := []Skill{
		{ID: 21, Name: "x", Status: "active", Measured: true, OfferedRuns: 40, LoadedRuns: 8},
		{ID: 22, Name: "y", Status: "active", Measured: true, OfferedRuns: 40, LoadedRuns: 8},
	}
	// Expected loser by the same sha256 rule the implementation uses.
	h21 := sha256.Sum256([]byte(strconv.FormatInt(21, 10)))
	h22 := sha256.Sum256([]byte(strconv.FormatInt(22, 10)))
	wantLoser := int64(22)
	if string(h21[:]) < string(h22[:]) {
		wantLoser = 21
	}

	var want []Decision
	for i := 0; i < 20; i++ {
		shuffled := append([]Skill(nil), skills...)
		rand.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		got := Evaluate(shuffled, Params{ProbationRuns: 25, Capacity: 1})
		if len(got) != 1 || got[0].ID != wantLoser || got[0].Reason != ReasonCapacity {
			t.Fatalf("shuffle %d: Evaluate() = %+v, want single eviction of %d", i, got, wantLoser)
		}
		if want == nil {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("shuffle %d: output %+v differs from first run %+v", i, got, want)
		}
	}
}

func TestLifecycle_PinnedExempt(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 31, Name: "pinned-zero", Status: "active", Pinned: true, Measured: true, OfferedRuns: 100, LoadedRuns: 0},
		{ID: 32, Name: "mature", Status: "active", Measured: true, OfferedRuns: 100, LoadedRuns: 1},
	}
	// Capacity 0: the pinned skill neither archives nor counts, so the one
	// counting skill is over capacity and goes.
	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 0})
	if len(got) != 1 || got[0].ID != 32 {
		t.Fatalf("Evaluate() = %+v, want only skill 32 evicted (pinned 31 exempt)", got)
	}
}

func TestLifecycle_IgnoresNonActive(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 41, Name: "disabled", Status: "disabled", Measured: true, OfferedRuns: 100, LoadedRuns: 0},
		{ID: 42, Name: "archived", Status: "archived", Measured: true, OfferedRuns: 100, LoadedRuns: 0},
		{ID: 43, Name: "bogus", Status: "whatever", Measured: true, OfferedRuns: 100, LoadedRuns: 0},
	}
	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 0})
	if len(got) != 0 {
		t.Fatalf("Evaluate() = %+v, want no decisions for non-active skills", got)
	}
}

func TestLifecycle_InputNotMutated(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{ID: 51, Name: "zero", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 0},
		{ID: 52, Name: "low", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 1},
		{ID: 53, Name: "high", Status: "active", Measured: true, OfferedRuns: 30, LoadedRuns: 29},
		{ID: 54, Name: "pinned", Status: "active", Pinned: true, Measured: true, OfferedRuns: 30, LoadedRuns: 0},
		{ID: 55, Name: "young", Status: "active", Measured: true, OfferedRuns: 1, LoadedRuns: 0},
		{ID: 56, Name: "off", Status: "disabled", Measured: false, OfferedRuns: 99, LoadedRuns: 0},
	}
	before := append([]Skill(nil), skills...)
	got := Evaluate(skills, Params{ProbationRuns: 25, Capacity: 1})
	if len(got) == 0 {
		t.Fatal("Evaluate() = no decisions, want some")
	}
	if !reflect.DeepEqual(skills, before) {
		t.Fatalf("input mutated:\n got %+v\nwant %+v", skills, before)
	}
	// Sanity: ordering is by ID ascending.
	for i := 1; i < len(got); i++ {
		if got[i-1].ID >= got[i].ID {
			t.Fatalf("output not sorted by ID: %+v", got)
		}
	}
}
