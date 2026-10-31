package steward

// StaleInput is the read-side state a review is judged against: what the
// review saw, what the task looks like now, and the newest review id.
type StaleInput struct {
	ReviewID          int64
	ReviewHeadSHA     string
	ReviewContractSha string
	HeadNow           string
	ContractShaNow    string
	LatestReviewID    int64
}

// Staleness reasons (AC20).
const (
	StaleHeadMoved       = "head moved"
	StaleContractChanged = "contract changed"
	StaleSuperseded      = "superseded by a newer review"
)

// StaleReason reports why a review is stale, or "" when it is fresh. An
// empty HeadNow is not a move (nothing is known about the head now); two
// empty contract shas are the same contract. Head movement is checked
// first, then a contract change, then a newer review.
func StaleReason(in StaleInput) string {
	if in.HeadNow != "" && in.HeadNow != in.ReviewHeadSHA {
		return StaleHeadMoved
	}
	if in.ContractShaNow != in.ReviewContractSha {
		return StaleContractChanged
	}
	if in.LatestReviewID != 0 && in.LatestReviewID != in.ReviewID {
		return StaleSuperseded
	}
	return ""
}

// MergeReady reports whether a review's verdict can be shown as merge-ready:
// only an ACCEPTED verdict on a finished review with no scope violation and
// no staleness. It is a label for a human's decision, never an action; the
// reason is non-empty when it is false.
func MergeReady(verdict, status string, scopeViolation bool, stale string) (bool, string) {
	switch {
	case verdict != VerdictAccepted:
		return false, "not accepted"
	case status != "done":
		return false, "review not finished"
	case scopeViolation:
		return false, "scope violation"
	case stale != "":
		return false, stale
	}
	return true, ""
}
