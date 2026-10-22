// Package attach validates, stores and renders prompt attachments
// (ADR-0038). It is self-contained: callers inject the workspace, forge and
// reuse lookups through Resolvers.
package attach

import (
	"encoding/json"
	"fmt"
)

// Attachment kinds.
const (
	KindImage   = "image"
	KindFile    = "file"
	KindFileRef = "file_ref"
	KindPR      = "pr"
	KindIssue   = "issue"
	KindReuse   = "reuse"
)

// Error codes carried by *Error.
const (
	CodeInvalid          = "invalid_attachment"
	CodeTooMany          = "too_many_attachments"
	CodeTooLarge         = "attachment_too_large"
	CodeUnsupportedImage = "unsupported_image_type"
	CodeMimeMismatch     = "mime_mismatch"
	CodeInvalidBase64    = "invalid_base64"
	CodeOutsideWorkspace = "outside_workspace"
	CodeNotFound         = "not_found"
	CodeForgeUnavailable = "forge_unavailable"
	CodeForgeNotFound    = "forge_not_found"
)

// Request is one client-sent attachment (wire form, ADR §1).
type Request struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	Path     string `json:"path,omitempty"`
	Number   int    `json:"number,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Forge    string `json:"forge,omitempty"`
	Line     int    `json:"line,omitempty"`
	EndLine  int    `json:"endLine,omitempty"`
	RunID    string `json:"runId,omitempty"`
	ID       string `json:"id,omitempty"`
}

// Limits bounds what Accept admits.
type Limits struct {
	MaxCount, MaxImageBytes, MaxFileBytes, MaxTotalBytes, MaxImageDim int
	MaxFileRefs, MaxForge, MaxPathLen, MaxNameBytes, MaxBodyBytes     int
}

// DefaultLimits returns the ADR-0038 limits.
func DefaultLimits() Limits {
	return Limits{
		MaxCount:      10,
		MaxImageBytes: 5 << 20,
		MaxFileBytes:  10 << 20,
		MaxTotalBytes: 10 << 20,
		MaxImageDim:   8000,
		MaxFileRefs:   20,
		MaxForge:      5,
		MaxPathLen:    4096,
		MaxNameBytes:  120,
		MaxBodyBytes:  32 << 10,
	}
}

// Error is the typed rejection. Index is the 0-based position in the request
// list, -1 when the error is not about one item.
type Error struct {
	Code    string
	Message string
	Index   int
}

func (e *Error) Error() string {
	if e.Index < 0 {
		return e.Message
	}
	return fmt.Sprintf("attachment %d: %s", e.Index+1, e.Message)
}

// RPCCode returns the wire error code.
func (e *Error) RPCCode() string { return e.Code }

func newErr(code string, index int, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...), Index: index}
}

// Item is the stored metadata of one accepted attachment.
type Item struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`

	Path    string `json:"path,omitempty"`
	IsDir   bool   `json:"isDir,omitempty"`
	Line    int    `json:"line,omitempty"`
	EndLine int    `json:"endLine,omitempty"`

	Forge   string `json:"forge,omitempty"`
	Number  int    `json:"number,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	State   string `json:"state,omitempty"`
	BaseRef string `json:"baseRef,omitempty"`
	HeadRef string `json:"headRef,omitempty"`

	// File is the basename inside the set directory. Stored, but stripped
	// from the wire by Public.
	File string `json:"file,omitempty"`
}

// Set is the accepted attachments of one prompt.
type Set struct {
	ID    string `json:"set"`
	Items []Item `json:"items"`
}

// Public returns a copy of the items with File cleared.
func (s Set) Public() []Item {
	out := make([]Item, len(s.Items))
	copy(out, s.Items)
	for i := range out {
		out[i].File = ""
	}
	return out
}

// EncodeSet serialises s as JSON; a Set with no items encodes as "".
func EncodeSet(s Set) string {
	if len(s.Items) == 0 {
		return ""
	}
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeSet is the inverse of EncodeSet; "" yields the zero Set.
func DecodeSet(raw string) (Set, error) {
	if raw == "" {
		return Set{}, nil
	}
	var s Set
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Set{}, fmt.Errorf("attach: decode set: %w", err)
	}
	return s, nil
}
