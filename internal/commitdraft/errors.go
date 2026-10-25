package commitdraft

// Error is a typed draft failure. Its Code is the wire code wsapi surfaces
// (it implements wsapi's rpcCoder), so wsapi needs no import of this package.
type Error struct {
	Code  string
	Msg   string
	Cause error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Msg + ": " + e.Cause.Error()
	}
	return e.Msg
}

// RPCCode is the wire code.
func (e *Error) RPCCode() string { return e.Code }

func (e *Error) Unwrap() error { return e.Cause }

// Is makes errors.Is(err, ErrX) match any *Error with ErrX's code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Typed failures; the code of each is the wire code.
var (
	ErrNothingStaged   = &Error{Code: "nothing_staged", Msg: "nothing is staged: stage changes first"}
	ErrNoDraftProvider = &Error{Code: "no_draft_provider", Msg: "no usable claude-native provider to draft with"}
	ErrRateLimited     = &Error{Code: "rate_limited", Msg: "too many drafts, try again shortly"}
	ErrBusy            = &Error{Code: "busy", Msg: "the draft runner is busy, try again"}
	ErrEmptyDraft      = &Error{Code: "empty_draft", Msg: "the model returned an empty message"}
	ErrDraftFailed     = &Error{Code: "draft_failed", Msg: "drafting failed"}
)

func withCause(base *Error, cause error) *Error {
	return &Error{Code: base.Code, Msg: base.Msg, Cause: cause}
}
