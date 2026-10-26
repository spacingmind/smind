// Package host runs a plugin's process (ADR-0042 section 4): it spawns it with
// a scrubbed environment, speaks LSP-framed JSON-RPC 2.0 with it over stdio,
// keeps a bounded log of what it printed, and supervises it (restart with
// backoff, a crash-loop breaker, a stop that leaves no orphan).
//
// A plugin is code the Human chose to run as themselves; nothing here is a
// sandbox. What this package does guarantee is that the daemon's own secrets
// are not handed over in the environment, that nothing a plugin writes can
// make the host allocate without bound, and that no process outlives its stop.
package host

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Frame error codes: the typed reason a plugin connection was closed.
const (
	FrameHeaderTooLarge = "header_too_large"
	FrameBadHeader      = "bad_header"
	FrameDuplicate      = "duplicate_header"
	FrameMissingLength  = "missing_length"
	FrameBadLength      = "bad_length"
	FrameBodyTooLarge   = "body_too_large"
	FrameTruncated      = "truncated"
	FrameBadJSON        = "bad_json"
	FrameBadMessage     = "bad_message"
	FrameWriteStalled   = "write_stalled"
)

// FrameError is a protocol violation by the peer. The connection is closed on
// any of them; the process is killed, because it is no longer trustworthy.
type FrameError struct {
	Code string
	Msg  string
}

func (e *FrameError) Error() string { return "plugin protocol: " + e.Code + ": " + e.Msg }

func frameErr(code, format string, a ...any) *FrameError {
	return &FrameError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// ErrClosed is returned by a call on a connection that has ended.
var ErrClosed = errors.New("plugin connection closed")

// Limits bound everything a peer can make the host read or hold.
type Limits struct {
	// MaxHeaderLine caps one header line, MaxHeaderLines their number.
	MaxHeaderLine  int
	MaxHeaderLines int
	// MaxBody caps a frame's body in bytes; the allocation happens only after
	// the declared length passed this check.
	MaxBody int
	// MaxOutstanding caps host-to-plugin requests waiting for an answer;
	// MaxInbound caps plugin-to-host requests being handled at once.
	MaxOutstanding int
	MaxInbound     int
}

// readFrame reads one `Content-Length`-framed message. io.EOF means the peer
// closed cleanly between frames; any other error is a *FrameError or an I/O
// error. r must have been built with a buffer of at least lim.MaxHeaderLine.
func readFrame(r *bufio.Reader, lim Limits) ([]byte, error) {
	length, seen := -1, map[string]bool{}
	for lines := 0; ; lines++ {
		if lines > lim.MaxHeaderLines {
			return nil, frameErr(FrameHeaderTooLarge, "more than %d header lines", lim.MaxHeaderLines)
		}
		line, err := r.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			return nil, frameErr(FrameHeaderTooLarge, "a header line is longer than %d bytes", lim.MaxHeaderLine)
		case err == io.EOF && len(line) == 0 && lines == 0:
			return nil, io.EOF
		case err != nil:
			if err == io.EOF {
				return nil, frameErr(FrameTruncated, "the stream ended inside the header")
			}
			return nil, err
		}
		if len(line) > lim.MaxHeaderLine {
			return nil, frameErr(FrameHeaderTooLarge, "a header line is longer than %d bytes", lim.MaxHeaderLine)
		}
		if len(line) < 2 || line[len(line)-2] != '\r' {
			return nil, frameErr(FrameBadHeader, "a header line must end with CRLF")
		}
		line = line[:len(line)-2]
		for _, b := range line {
			if b < 0x20 || b >= 0x7f {
				return nil, frameErr(FrameBadHeader, "a header line holds a control or non-ASCII byte")
			}
		}
		if len(line) == 0 { // the blank line ends the header
			break
		}
		name, value, ok := strings.Cut(string(line), ":")
		if !ok {
			return nil, frameErr(FrameBadHeader, "a header line has no colon")
		}
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "content-length" && key != "content-type" {
			return nil, frameErr(FrameBadHeader, "unknown header %q", name)
		}
		if seen[key] {
			return nil, frameErr(FrameDuplicate, "header %q appears twice", name)
		}
		seen[key] = true
		if key == "content-length" {
			n, err := parseLength(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			length = n
		}
	}
	switch {
	case length < 0:
		return nil, frameErr(FrameMissingLength, "no Content-Length header")
	case length == 0:
		return nil, frameErr(FrameBadLength, "Content-Length is 0")
	case length > lim.MaxBody:
		return nil, frameErr(FrameBodyTooLarge, "Content-Length %d is over the %d byte cap", length, lim.MaxBody)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, frameErr(FrameTruncated, "the stream ended inside a %d byte body", length)
		}
		return nil, err
	}
	return body, nil
}

// parseLength accepts only 1 to 10 ASCII digits: no sign, no space, no hex.
func parseLength(s string) (int, error) {
	if s == "" || len(s) > 10 {
		return 0, frameErr(FrameBadLength, "Content-Length %q is not a plain number", clip(s))
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return 0, frameErr(FrameBadLength, "Content-Length %q is not a plain number", clip(s))
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > int64(^uint32(0)>>1) {
		return 0, frameErr(FrameBadLength, "Content-Length %q is out of range", clip(s))
	}
	return int(n), nil
}

func clip(s string) string {
	if len(s) > 24 {
		return s[:24] + "..."
	}
	return s
}

// writeFrame writes body as one frame in a single Write.
func writeFrame(w io.Writer, body []byte) error {
	buf := make([]byte, 0, len(body)+32)
	buf = append(buf, "Content-Length: "...)
	buf = strconv.AppendInt(buf, int64(len(body)), 10)
	buf = append(buf, "\r\n\r\n"...)
	buf = append(buf, body...)
	_, err := w.Write(buf)
	return err
}
