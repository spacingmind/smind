package plugins

import (
	"errors"
	"fmt"
)

// Typed error codes. They cross the wsapi unchanged, so a client (and a test)
// can tell a pinning failure from an unsafe archive without parsing text.
const (
	CodeUnpinnedRef     = "unpinned_ref"     // git source without a 40-hex commit pin
	CodeUnpinnedArchive = "unpinned_archive" // archive source without #sha256=
	CodeHashMismatch    = "hash_mismatch"    // archive bytes differ from the pin
	CodeUnsafeEntry     = "unsafe_entry"     // path escape, absolute, drive letter, NUL, device
	CodeLinkRejected    = "link_rejected"    // symlink or hardlink in a source
	CodeTooLarge        = "too_large"        // size cap exceeded (counted while streaming)
	CodeTooManyEntries  = "too_many_entries" // entry cap exceeded
	CodeDuplicateEntry  = "duplicate_entry"  // same name twice, or names differing only in case
	CodeBadArchive      = "bad_archive"      // unreadable or unsupported archive
	CodeBadSource       = "bad_source"       // the source string is not usable
	CodeGit             = "git_failed"       // fetching or verifying the commit failed
	CodeInstalled       = "already_installed"
	CodeNotFound        = "not_found"
	CodeLayout          = "layout_conflict"
	CodeSwitchOff       = "plugins_disabled" // plugins.enabled is false
	CodeConsent         = "consent_required" // enable without the matching confirmation
	CodePending         = "update_pending"   // an update waits for re-consent
	CodeNoPending       = "no_pending_update"
	CodeBadManifest     = "bad_manifest"
	CodeBusy            = "busy"
)

// Error is a typed plugin error.
type Error struct {
	Code string
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.Err)
	}
	return e.Code + ": " + e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func newErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// CodeOf returns the typed code of err, or "" when it carries none. A
// *ManifestError reports CodeBadManifest.
func CodeOf(err error) string {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	var me *ManifestError
	if errors.As(err, &me) {
		return CodeBadManifest
	}
	return ""
}
