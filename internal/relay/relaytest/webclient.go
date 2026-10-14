package relaytest

// webclient.go is a grpc-web implementation of relaypb.RelayClient: unary
// RPCs over HTTP POST framing, the two bidi RPCs over the websocket framing
// traefik/grpc-web's WithWebsockets(true) serves. It follows
// internal/relay/server/grpcweb_test.go's hand-rolled client (which lives in
// a _test file and cannot be imported) but returns errors as gRPC statuses and
// implements grpc.BidiStreamingClient, so internal/relay/client.Admit,
// OpenControl and OpenData -- and the E2EE layer on top -- run over it
// unchanged.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"nhooyr.io/websocket"

	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

const (
	webService = "/relay.v1.Relay/"
	// maxWebMessage bounds one websocket message; the relay caps frames far
	// below it, the library default (32 KiB) is just too small for a buffered
	// burst.
	maxWebMessage = 8 << 20
)

type webClient struct {
	httpBase string // https://host:port
	wsBase   string // wss://host:port
	hc       *http.Client
}

func newWebClient(addr string, tlsCfg *tls.Config) *webClient {
	return &webClient{
		httpBase: "https://" + addr,
		wsBase:   "wss://" + addr,
		// HTTP/1.1 (custom TLS config, no ForceAttemptHTTP2): the websocket
		// upgrade needs it. No client Timeout: it would also bound the
		// long-lived websocket's HTTP request.
		hc: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}},
	}
}

func (c *webClient) close() { c.hc.CloseIdleConnections() }

var _ relaypb.RelayClient = (*webClient)(nil)

func (c *webClient) AdmitChallenge(ctx context.Context, in *relaypb.AdmitChallengeRequest, _ ...grpc.CallOption) (*relaypb.AdmitChallengeResponse, error) {
	out := &relaypb.AdmitChallengeResponse{}
	if err := c.unary(ctx, "AdmitChallenge", in, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *webClient) Admit(ctx context.Context, in *relaypb.AdmitRequest, _ ...grpc.CallOption) (*relaypb.AdmitResponse, error) {
	out := &relaypb.AdmitResponse{}
	if err := c.unary(ctx, "Admit", in, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *webClient) OpenControl(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[relaypb.ControlFrame, relaypb.ControlFrame], error) {
	w, err := c.openStream(ctx, "OpenControl")
	if err != nil {
		return nil, err
	}
	return newWebStream[relaypb.ControlFrame, relaypb.ControlFrame](w), nil
}

func (c *webClient) OpenData(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[relaypb.Frame, relaypb.Frame], error) {
	w, err := c.openStream(ctx, "OpenData")
	if err != nil {
		return nil, err
	}
	return newWebStream[relaypb.Frame, relaypb.Frame](w), nil
}

// --- unary ---

func (c *webClient) unary(ctx context.Context, method string, in, out proto.Message) error {
	body, err := lpm(in)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.httpBase+webService+method, bytes.NewReader(body))
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	req.Header.Set("content-type", "application/grpc-web+proto")
	req.Header.Set("x-grpc-web", "1")
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		for k, vs := range md {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return statusFromTransport(ctx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return statusFromTransport(ctx, err)
	}
	// A trailers-only response carries the status in the HTTP headers.
	if s := resp.Header.Get("Grpc-Status"); s != "" {
		return trailerError(s, resp.Header.Get("Grpc-Message"))
	}
	var tr *trailer
	got := false
	for len(data) > 0 {
		if len(data) < 5 {
			return status.Error(codes.Internal, "grpc-web: truncated frame header")
		}
		flag, n := data[0], binary.BigEndian.Uint32(data[1:5])
		if uint32(len(data)-5) < n {
			return status.Error(codes.Internal, "grpc-web: truncated frame body")
		}
		payload := data[5 : 5+n]
		data = data[5+n:]
		if flag&0x80 != 0 {
			if t, err := parseTrailer(payload); err == nil && t.status != "" {
				tr = t
			}
			continue
		}
		if err := proto.Unmarshal(payload, out); err != nil {
			return status.Error(codes.Internal, "grpc-web: bad response message")
		}
		got = true
	}
	if tr != nil {
		if err := trailerError(tr.status, tr.message); err != nil {
			return err
		}
	}
	if !got {
		return status.Errorf(codes.Internal, "grpc-web: no response message (http %d)", resp.StatusCode)
	}
	return nil
}

// --- bidi over websocket ---

// webConn is the untyped websocket stream core.
type webConn struct {
	ctx  context.Context
	conn *websocket.Conn

	readMu  sync.Mutex
	pending []byte // bytes of the websocket messages not yet consumed as frames
	termMu  sync.Mutex
	term    error // sticky end of stream: io.EOF for OK, else a status error

	closeOnce sync.Once
	sendOnce  sync.Once
}

func (c *webClient) openStream(ctx context.Context, method string) (*webConn, error) {
	conn, _, err := websocket.Dial(ctx, c.wsBase+webService+method, &websocket.DialOptions{
		HTTPClient:   c.hc,
		Subprotocols: []string{"grpc-websockets"},
	})
	if err != nil {
		return nil, statusFromTransport(ctx, err)
	}
	conn.SetReadLimit(maxWebMessage)
	w := &webConn{ctx: ctx, conn: conn}
	// A cancelled stream context ends the stream, like on native gRPC.
	context.AfterFunc(ctx, w.closeConn)

	// The first message carries the request metadata as HTTP header text.
	var hdr bytes.Buffer
	hdr.WriteString("content-type: application/grpc-web+proto\r\n")
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		for k, vs := range md {
			for _, v := range vs {
				fmt.Fprintf(&hdr, "%s: %s\r\n", k, v)
			}
		}
	}
	if err := conn.Write(ctx, websocket.MessageBinary, hdr.Bytes()); err != nil {
		w.closeConn()
		return nil, statusFromTransport(ctx, err)
	}
	return w, nil
}

func (w *webConn) closeConn() {
	w.closeOnce.Do(func() { _ = w.conn.Close(websocket.StatusNormalClosure, "") })
}

func (w *webConn) terminal() error {
	w.termMu.Lock()
	defer w.termMu.Unlock()
	return w.term
}

func (w *webConn) setTerminal(err error) error {
	w.termMu.Lock()
	if w.term == nil {
		w.term = err
	}
	err = w.term
	w.termMu.Unlock()
	return err
}

func (w *webConn) sendMsg(m proto.Message) error {
	if err := w.terminal(); err != nil {
		return io.EOF // gRPC semantics: Send on an ended stream is io.EOF, Recv has the cause
	}
	b, err := lpm(m)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	// Leading control byte 0 = data follows (1 = client finished sending).
	if err := w.conn.Write(w.ctx, websocket.MessageBinary, append([]byte{0}, b...)); err != nil {
		if w.terminal() != nil {
			return io.EOF
		}
		return statusFromTransport(w.ctx, err)
	}
	return nil
}

func (w *webConn) closeSend() error {
	w.sendOnce.Do(func() { _ = w.conn.Write(w.ctx, websocket.MessageBinary, []byte{1}) })
	return nil
}

// fill appends one more websocket message to pending.
func (w *webConn) fill() error {
	typ, data, err := w.conn.Read(w.ctx)
	if err != nil {
		return statusFromTransport(w.ctx, err)
	}
	if typ == websocket.MessageBinary {
		w.pending = append(w.pending, data...)
	}
	return nil
}

// readFrame reassembles one flag+payload record: the server writes a frame's
// 5-byte header and its payload as separate websocket messages.
func (w *webConn) readFrame() (byte, []byte, error) {
	for len(w.pending) < 5 {
		if err := w.fill(); err != nil {
			return 0, nil, err
		}
	}
	flag, n := w.pending[0], binary.BigEndian.Uint32(w.pending[1:5])
	for uint32(len(w.pending)-5) < n {
		if err := w.fill(); err != nil {
			return 0, nil, err
		}
	}
	payload := append([]byte(nil), w.pending[5:5+n]...)
	w.pending = w.pending[5+n:]
	return flag, payload, nil
}

// recvInto decodes the next message into m. The end of the stream is the
// trailer's status: io.EOF for OK, else that status as an error -- the same
// shape a native gRPC stream's Recv gives.
func (w *webConn) recvInto(m proto.Message) error {
	w.readMu.Lock()
	defer w.readMu.Unlock()
	if err := w.terminal(); err != nil {
		return err
	}
	for {
		flag, payload, err := w.readFrame()
		if err != nil {
			return w.end(err)
		}
		if flag&0x80 == 0 {
			if err := proto.Unmarshal(payload, m); err != nil {
				return w.end(status.Error(codes.Internal, "grpc-web: bad message"))
			}
			return nil
		}
		// Header frames and the trailer frame share the 0x80 encoding; only
		// the trailer carries grpc-status.
		tr, err := parseTrailer(payload)
		if err != nil || tr.status == "" {
			continue
		}
		if e := trailerError(tr.status, tr.message); e != nil {
			return w.end(e)
		}
		return w.end(io.EOF)
	}
}

func (w *webConn) end(err error) error {
	err = w.setTerminal(err)
	go w.closeConn() // closing does a close handshake; never block Recv on it
	return err
}

// webStream types a webConn as a grpc.BidiStreamingClient.
type msgPtr[T any] interface {
	*T
	proto.Message
}

type webStream[Req, Resp any, PReq msgPtr[Req], PResp msgPtr[Resp]] struct{ w *webConn }

func newWebStream[Req, Resp any, PReq msgPtr[Req], PResp msgPtr[Resp]](w *webConn) *webStream[Req, Resp, PReq, PResp] {
	return &webStream[Req, Resp, PReq, PResp]{w: w}
}

func (s *webStream[Req, Resp, PReq, PResp]) Send(m *Req) error { return s.w.sendMsg(PReq(m)) }

func (s *webStream[Req, Resp, PReq, PResp]) Recv() (*Resp, error) {
	m := new(Resp)
	if err := s.w.recvInto(PResp(m)); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *webStream[Req, Resp, PReq, PResp]) Header() (metadata.MD, error) { return nil, nil }
func (s *webStream[Req, Resp, PReq, PResp]) Trailer() metadata.MD         { return nil }
func (s *webStream[Req, Resp, PReq, PResp]) CloseSend() error             { return s.w.closeSend() }
func (s *webStream[Req, Resp, PReq, PResp]) Context() context.Context     { return s.w.ctx }

func (s *webStream[Req, Resp, PReq, PResp]) SendMsg(m any) error {
	pm, ok := m.(proto.Message)
	if !ok {
		return status.Error(codes.Internal, "grpc-web: not a proto message")
	}
	return s.w.sendMsg(pm)
}

func (s *webStream[Req, Resp, PReq, PResp]) RecvMsg(m any) error {
	pm, ok := m.(proto.Message)
	if !ok {
		return status.Error(codes.Internal, "grpc-web: not a proto message")
	}
	return s.w.recvInto(pm)
}

// --- framing helpers ---

// lpm is one length-prefixed message: flag byte 0, 4-byte big-endian length,
// the protobuf.
func lpm(m proto.Message) ([]byte, error) {
	b, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 5+len(b))
	binary.BigEndian.PutUint32(out[1:5], uint32(len(b)))
	copy(out[5:], b)
	return out, nil
}

type trailer struct{ status, message string }

func parseTrailer(payload []byte) (*trailer, error) {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(append([]byte(nil), payload...), '\r', '\n'))))
	h, err := tp.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return nil, err
	}
	return &trailer{status: h.Get("Grpc-Status"), message: h.Get("Grpc-Message")}, nil
}

// trailerError turns grpc-status / grpc-message into a status error (nil for
// OK).
func trailerError(code, message string) error {
	n, err := strconv.Atoi(code)
	if err != nil {
		return status.Errorf(codes.Internal, "grpc-web: bad grpc-status %q", code)
	}
	if n == int(codes.OK) {
		return nil
	}
	if m, err := url.PathUnescape(message); err == nil {
		message = m
	}
	return status.Error(codes.Code(n), message)
}

// statusFromTransport maps a transport error to a gRPC status; a cancelled or
// expired context keeps its own code like on native gRPC.
func statusFromTransport(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	return status.Error(codes.Unavailable, err.Error())
}
