package steward

import (
	"strings"
	"testing"
)

// freshInput is a review whose task looks exactly as it did when the review
// ran: same head, same contract, still the newest review.
func freshInput() StaleInput {
	return StaleInput{
		ReviewID:          3,
		ReviewHeadSHA:     strings.Repeat("a", 40),
		ReviewContractSha: "contract-v1",
		HeadNow:           strings.Repeat("a", 40),
		ContractShaNow:    "contract-v1",
		LatestReviewID:    3,
	}
}

func TestReviewStale_HeadMoves(t *testing.T) {
	if got := StaleReason(freshInput()); got != "" {
		t.Errorf("StaleReason(fresh) = %q, want empty", got)
	}

	moved := freshInput()
	moved.HeadNow = strings.Repeat("b", 40)
	if got := StaleReason(moved); got != StaleHeadMoved {
		t.Errorf("StaleReason(head moved) = %q, want %q", got, StaleHeadMoved)
	}

	// An unknown current head is not a move.
	unknown := freshInput()
	unknown.HeadNow = ""
	if got := StaleReason(unknown); got != "" {
		t.Errorf("StaleReason(head now unknown) = %q, want empty", got)
	}

	// A head move outranks a contract change and a newer review.
	both := freshInput()
	both.HeadNow = strings.Repeat("b", 40)
	both.ContractShaNow = "contract-v2"
	both.LatestReviewID = 9
	if got := StaleReason(both); got != StaleHeadMoved {
		t.Errorf("StaleReason(head, contract, newer) = %q, want %q", got, StaleHeadMoved)
	}
}

func TestReviewStale_NewContractVersion(t *testing.T) {
	changed := freshInput()
	changed.ContractShaNow = "contract-v2"
	if got := StaleReason(changed); got != StaleContractChanged {
		t.Errorf("StaleReason(contract changed) = %q, want %q", got, StaleContractChanged)
	}

	// Both empty means the same (no contract on either side).
	none := freshInput()
	none.ReviewContractSha = ""
	none.ContractShaNow = ""
	if got := StaleReason(none); got != "" {
		t.Errorf("StaleReason(no contract either side) = %q, want empty", got)
	}

	// A contract change outranks a newer review; a head move outranks it.
	contractAndNewer := freshInput()
	contractAndNewer.ContractShaNow = "contract-v2"
	contractAndNewer.LatestReviewID = 9
	if got := StaleReason(contractAndNewer); got != StaleContractChanged {
		t.Errorf("StaleReason(contract, newer) = %q, want %q", got, StaleContractChanged)
	}
}

func TestReviewStale_NewerReview(t *testing.T) {
	newer := freshInput()
	newer.LatestReviewID = 4
	if got := StaleReason(newer); got != StaleSuperseded {
		t.Errorf("StaleReason(newer review) = %q, want %q", got, StaleSuperseded)
	}

	same := freshInput()
	same.LatestReviewID = same.ReviewID
	if got := StaleReason(same); got != "" {
		t.Errorf("StaleReason(same review) = %q, want empty", got)
	}

	// Zero means no newer review is known.
	unknown := freshInput()
	unknown.LatestReviewID = 0
	if got := StaleReason(unknown); got != "" {
		t.Errorf("StaleReason(no latest known) = %q, want empty", got)
	}
}

func TestMergeReady_OnlyAcceptedDoneFresh(t *testing.T) {
	cases := []struct {
		name           string
		verdict        string
		status         string
		scopeViolation bool
		stale          string
		want           bool
		wantReason     string
	}{
		{"accepted done fresh", VerdictAccepted, "done", false, "", true, ""},
		{"changes is not merge ready", VerdictChanges, "done", false, "", false, "not accepted"},
		{"blocked is not merge ready", VerdictBlocked, "done", false, "", false, "not accepted"},
		{"empty verdict is not merge ready", "", "done", false, "", false, "not accepted"},
		{"review still running", VerdictAccepted, "running", false, "", false, "review not finished"},
		{"review errored", VerdictAccepted, "error", false, "", false, "review not finished"},
		{"scope violation", VerdictAccepted, "done", true, "", false, "scope violation"},
		{"stale head", VerdictAccepted, "done", false, StaleHeadMoved, false, StaleHeadMoved},
		{"stale contract", VerdictAccepted, "done", false, StaleContractChanged, false, StaleContractChanged},
		{"superseded", VerdictAccepted, "done", false, StaleSuperseded, false, StaleSuperseded},
		{"not accepted wins over the rest", VerdictChanges, "running", true, StaleSuperseded, false, "not accepted"},
		{"unfinished wins over stale", VerdictAccepted, "running", false, StaleSuperseded, false, "review not finished"},
		{"scope violation wins over stale", VerdictAccepted, "done", true, StaleSuperseded, false, "scope violation"},
	}
	for _, tc := range cases {
		got, reason := MergeReady(tc.verdict, tc.status, tc.scopeViolation, tc.stale)
		if got != tc.want || reason != tc.wantReason {
			t.Errorf("%s: MergeReady = (%v, %q), want (%v, %q)", tc.name, got, reason, tc.want, tc.wantReason)
		}
	}
}
