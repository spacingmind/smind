package runsock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// The wire protocol of a run's browser socket: newline-delimited JSON, one
// request per line, one reply per request, one request in flight per connection.
// The socket can call exactly one thing (a browser tool of the run it belongs
// to), so there is no method name: the request names a tool and its arguments,
// nothing else. Unknown fields, a missing tool, malformed JSON or a line over
// MaxRequestBytes close the connection.

const (
	// MaxRequestBytes is the longest request line (a tool call is a few hundred
	// bytes). Replies are not capped here: the broker's own caps apply.
	MaxRequestBytes = 16 << 10
	// MaxToolNameBytes bounds the tool name.
	MaxToolNameBytes = 64
)

// The deadlines are variables so a test can shorten them.
var (
	idleTimeout  = 5 * time.Minute
	writeTimeout = 30 * time.Second
)

// Request is one browser tool call.
type Request struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is the reply: the handler's result, or an error text.
type Response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Handler runs one request for the run the socket belongs to. The run and the
// workspace are bound by whoever created the socket: nothing in a request can
// name another.
type Handler func(ctx context.Context, req Request) (any, error)

var errBadRequest = errors.New("runsock: bad request")

// decodeRequest parses one request line strictly.
func decodeRequest(line []byte) (Request, error) {
	var req Request
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return Request{}, errBadRequest
	}
	if dec.More() { // trailing data after the object
		return Request{}, errBadRequest
	}
	if req.Tool == "" || len(req.Tool) > MaxToolNameBytes {
		return Request{}, errBadRequest
	}
	return req, nil
}

// serveConn answers requests on conn until it closes, a request is bad, the
// connection idles out, or ctx ends. It never reads before the caller has
// checked the peer.
// deadlineConn is what serveConn needs: a net.Conn on unix sockets, an emulation
// on Windows pipes.
type deadlineConn interface {
	io.ReadWriteCloser
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

func serveConn(ctx context.Context, conn deadlineConn, handle Handler) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, MaxRequestBytes+2)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		line, err := br.ReadSlice('\n')
		if err != nil { // EOF, idle timeout, a line over the cap (ErrBufferFull), a reset
			return
		}
		if len(line) > MaxRequestBytes+1 { // the line plus its newline
			return
		}
		req, err := decodeRequest(line)
		if err != nil {
			return
		}
		out := Response{}
		if res, herr := handle(ctx, req); herr != nil {
			out.Error = herr.Error()
		} else {
			out.Result = res
		}
		b, merr := json.Marshal(out)
		if merr != nil {
			b, _ = json.Marshal(Response{Error: "the reply could not be encoded"})
		}
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, werr := conn.Write(append(b, '\n')); werr != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// ReadResponse reads one reply line (used by the child and by tests).
func ReadResponse(r io.Reader) (Response, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(line, &out); err != nil {
		return Response{}, err
	}
	return Response{Result: out.Result, Error: out.Error}, nil
}
