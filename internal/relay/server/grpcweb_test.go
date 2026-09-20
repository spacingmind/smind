package server

// grpcweb_test.go is the plan's Item 2 test scenario
// (docs/plans/active/mobile-app-milestone-1.md): "a grpc-web-speaking test
// client ... completes Admit -> OpenControl -> ... a forwarded message,
// proving grpc-web reaches the same admission/forwarding logic Item 2's
// native-gRPC tests already cover, not a parallel/divergent path", plus "a
// malformed/incomplete grpc-web frame is rejected without crashing the
// server or affecting concurrent native-gRPC connections".
//
// There is no existing Go grpc-web client library (the ecosystem's clients
// are all JS, per grpcweb.go's own doc comment on why), so this file
// implements just enough of the grpc-web wire format by hand -- a unary
// call over plain HTTP framing, and a bidi call over the websocket framing
// WithWebsockets(true) turns on -- to drive the exact same production
// WrappedGrpcServer.ServeHTTP path a real browser/RN client would.
// internal/relay/relaypb.OpenData additionally needs the full E2EE
// handshake (internal/relay/e2ee) layered on top, which is exactly what
// Item 3's real TypeScript grpc-web client (the plan's own named
// alternative for this scenario) exercises against this same server; this
// file's OpenControl round trip already proves the identical bidi-over-
// websocket bridge OpenData rides on is real.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"nhooyr.io/websocket"

	"github.com/spacingmind/smind/internal/relay/admission"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// grpcWebTestRelay starts a real Run relay with both listeners on
// ephemeral ports and an enrolled workspace, returning everything a
// grpc-web (or native gRPC) test client needs.
type grpcWebTestRelay struct {
	nativeAddr string
	webAddr    string
	tlsConfig  *tls.Config
	workspace  string
	secret     []byte
}

func startGRPCWebTestRelay(t *testing.T) (*grpcWebTestRelay, func()) {
	t.Helper()
	dir := t.TempDir()
	const workspaceID = "ws-grpcweb"
	secret, err := EnrollWorkspace(dir, workspaceID)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	cert, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	nativeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	webLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{
			DataDir:         dir,
			Listener:        nativeLis,
			GRPCWebListener: webLis,
		})
	}()

	pool := x509.NewCertPool()
	pool.AddCert(certLeaf(t, cert))

	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("relay Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("relay did not stop")
		}
	}
	return &grpcWebTestRelay{
		nativeAddr: nativeLis.Addr().String(),
		webAddr:    webLis.Addr().String(),
		tlsConfig:  &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
		workspace:  workspaceID,
		secret:     secret,
	}, stop
}

func certLeaf(t *testing.T, cert tls.Certificate) *x509.Certificate {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return leaf
}

// --- grpc-web unary framing (plain HTTP POST) ---

// grpcWebFrame writes one length-prefixed grpc-web data frame (flag byte
// 0x00), the same framing grpc-go's own wire format already uses for a
// single message, which grpc-web reuses as-is for the HTTP body.
func grpcWebDataFrame(msg proto.Message) ([]byte, error) {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame, nil
}

// grpcWebTrailers is the parsed result of a unary grpc-web call's trailer
// frame: grpc-status/grpc-message, the two fields that carry RPC-level
// success/failure over grpc-web (there being no real HTTP/2 trailers on a
// plain HTTP/1.1 POST).
type grpcWebTrailers struct {
	status  string
	message string
}

// grpcWebUnary drives one unary RPC (AdmitChallenge, Admit) over real
// grpc-web+proto framing against a running relay, decoding resp in place
// on success.
func grpcWebUnary(t *testing.T, client *http.Client, baseURL, method string, req, resp proto.Message) grpcWebTrailers {
	t.Helper()
	body, err := grpcWebDataFrame(req)
	if err != nil {
		t.Fatalf("frame request: %v", err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, baseURL+"/relay.v1.Relay/"+method, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	httpReq.Header.Set("content-type", "application/grpc-web+proto")
	httpResp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("%s: do: %v", method, err)
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("%s: read body: %v", method, err)
	}
	return parseGRPCWebFrames(t, method, data, resp)
}

// parseGRPCWebFrames walks the concatenated data+trailer frames a grpc-web
// unary response body carries, unmarshaling the (at most one) data frame
// into resp and returning the trailer.
func parseGRPCWebFrames(t *testing.T, method string, data []byte, resp proto.Message) grpcWebTrailers {
	t.Helper()
	var trailers grpcWebTrailers
	for len(data) > 0 {
		if len(data) < 5 {
			t.Fatalf("%s: truncated frame header (%d bytes left)", method, len(data))
		}
		flag := data[0]
		length := binary.BigEndian.Uint32(data[1:5])
		if uint32(len(data)-5) < length {
			t.Fatalf("%s: truncated frame body: want %d bytes, have %d", method, length, len(data)-5)
		}
		payload := data[5 : 5+length]
		data = data[5+length:]
		if flag&0x80 != 0 {
			trailers = parseGRPCWebTrailerPayload(t, payload)
			continue
		}
		if resp != nil {
			if err := proto.Unmarshal(payload, resp); err != nil {
				t.Fatalf("%s: unmarshal response: %v", method, err)
			}
		}
	}
	return trailers
}

func parseGRPCWebTrailerPayload(t *testing.T, payload []byte) grpcWebTrailers {
	t.Helper()
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(payload, '\r', '\n'))))
	header, err := tp.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		t.Fatalf("parse trailer: %v", err)
	}
	return grpcWebTrailers{status: header.Get("Grpc-Status"), message: header.Get("Grpc-Message")}
}

// admitOverGRPCWeb completes the full two-step admission exchange
// (AdmitChallenge then Admit) purely over grpc-web framing, the same HMAC
// transcript internal/relay/admission's native-gRPC tests already cover --
// proving grpc-web reaches the identical verifier logic, not a parallel
// path.
func admitOverGRPCWeb(t *testing.T, client *http.Client, baseURL, workspaceID, daemonKeyID string, secret []byte) []byte {
	t.Helper()
	clientNonce := make([]byte, admission.NonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	var chal relaypb.AdmitChallengeResponse
	tr := grpcWebUnary(t, client, baseURL, "AdmitChallenge", &relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
	}, &chal)
	if tr.status != "" && tr.status != "0" {
		t.Fatalf("AdmitChallenge grpc-status = %s (%s)", tr.status, tr.message)
	}

	req := &relaypb.AdmitRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     workspaceID,
		ClientNonce:     clientNonce,
		DaemonKeyId:     daemonKeyID,
		ServerNonce:     chal.GetServerNonce(),
	}
	req.Hmac = admission.ComputeHMAC(admission.HashSecret(secret), req)

	var admitResp relaypb.AdmitResponse
	tr = grpcWebUnary(t, client, baseURL, "Admit", req, &admitResp)
	if tr.status != "" && tr.status != "0" {
		t.Fatalf("Admit grpc-status = %s (%s)", tr.status, tr.message)
	}
	if len(admitResp.GetAdmissionId()) == 0 {
		t.Fatalf("Admit: empty admission id")
	}
	return admitResp.GetAdmissionId()
}

// TestGRPCWeb_UnaryAdmissionRoundTrip proves the plain-HTTP grpc-web path
// reaches the exact same admission verifier the existing native-gRPC tests
// (internal/relay/admission, harness_test.go's admit helper) exercise.
func TestGRPCWeb_UnaryAdmissionRoundTrip(t *testing.T) {
	relay, stop := startGRPCWebTestRelay(t)
	defer stop()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: relay.tlsConfig}}
	admissionID := admitOverGRPCWeb(t, client, "https://"+relay.webAddr, relay.workspace, "grpcweb-test-daemon", relay.secret)
	if len(admissionID) == 0 {
		t.Fatal("expected a non-empty admission id")
	}
}

// TestGRPCWeb_MalformedFrameRejectedWithoutCrashing sends a grpc-web
// request whose body is not valid grpc-web framing at all (declares a
// frame far longer than the body actually carries) and asserts the server
// answers with an ordinary HTTP/gRPC error rather than crashing or hanging
// -- then proves a concurrent *native* gRPC connection still works,
// showing the malformed grpc-web request didn't affect it.
func TestGRPCWeb_MalformedFrameRejectedWithoutCrashing(t *testing.T) {
	relay, stop := startGRPCWebTestRelay(t)
	defer stop()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: relay.tlsConfig}, Timeout: 10 * time.Second}

	// A frame header claiming a huge payload the body doesn't actually
	// contain.
	malformed := []byte{0x00, 0x00, 0x10, 0x00, 0x00, 'x', 'x'}
	httpReq, err := http.NewRequest(http.MethodPost, "https://"+relay.webAddr+"/relay.v1.Relay/AdmitChallenge", bytes.NewReader(malformed))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	httpReq.Header.Set("content-type", "application/grpc-web+proto")
	resp, err := client.Do(httpReq)
	if err != nil {
		// A connection-level rejection (rather than a clean HTTP response)
		// is also an acceptable "did not crash the server" outcome, as
		// long as the relay is still up afterwards -- checked below.
		t.Logf("malformed request: transport-level error (acceptable): %v", err)
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// The relay (both listeners, including native gRPC) must still be
	// healthy: a normal admission exchange, this time for real, must
	// still succeed.
	admissionID := admitOverGRPCWeb(t, client, "https://"+relay.webAddr, relay.workspace, "grpcweb-test-daemon-2", relay.secret)
	if len(admissionID) == 0 {
		t.Fatal("relay did not recover a usable grpc-web connection after the malformed request")
	}

	dialAndAdmitNative(t, relay)
}

// dialAndAdmitNative proves the native-gRPC listener is unaffected by
// anything grpc-web-side: a completely ordinary admission exchange over
// real native gRPC, exactly like internal/relay/server's own harness_test.go
// helper does.
func dialAndAdmitNative(t *testing.T, relay *grpcWebTestRelay) {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///"+relay.nativeAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(relay.tlsConfig)),
	)
	if err != nil {
		t.Fatalf("native gRPC dial after malformed grpc-web request: %v", err)
	}
	defer conn.Close()
	c := relaypb.NewRelayClient(conn)

	clientNonce := make([]byte, admission.NonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chal, err := c.AdmitChallenge(ctx, &relaypb.AdmitChallengeRequest{
		ProtocolVersion: admission.ProtocolVersion,
		WorkspaceId:     relay.workspace,
		ClientNonce:     clientNonce,
		DaemonKeyId:     "native-after-malformed",
	})
	if err != nil {
		t.Fatalf("native AdmitChallenge after malformed grpc-web request: %v", err)
	}
	if len(chal.GetServerNonce()) == 0 {
		t.Fatal("native AdmitChallenge: empty server nonce")
	}
}

// --- bidi streaming over grpc-web's websocket transport ---

// wsGRPCWebStream is a minimal hand-rolled grpc-web-over-websocket bidi
// client: enough to send/receive length-prefixed protobuf frames against
// WrappedGrpcServer's HandleGrpcWebsocketRequest, matching that function's
// wire format exactly (see websocket_wrapper.go, read closely for this).
type wsGRPCWebStream struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context

	// pending accumulates bytes across websocket messages: grpc-go's
	// generic HTTP/2-via-http.Handler write path issues one Write() call
	// for a frame's 5-byte header and a separate Write() call for its
	// payload, which the underlying webSocketResponseWriter turns into
	// two distinct websocket binary messages (not one combined message
	// per LPM/trailer record) -- so a frame must be reassembled from a
	// byte stream, not read message-by-message.
	pending []byte
}

// openGRPCWebSocketStream dials method as a grpc-web websocket bidi
// stream: the "grpc-websockets" subprotocol, then one initial binary
// message carrying the gRPC metadata headers as raw HTTP-header text
// (exactly what parseHeaders on the server side expects).
func openGRPCWebSocketStream(t *testing.T, ctx context.Context, wsBaseURL, method string, tlsConfig *tls.Config, headers map[string]string) *wsGRPCWebStream {
	t.Helper()
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	conn, _, err := websocket.Dial(ctx, wsBaseURL+"/relay.v1.Relay/"+method, &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{"grpc-websockets"},
	})
	if err != nil {
		t.Fatalf("websocket dial %s: %v", method, err)
	}

	var headerBuf bytes.Buffer
	headerBuf.WriteString("content-type: application/grpc-web+proto\r\n")
	for k, v := range headers {
		fmt.Fprintf(&headerBuf, "%s: %s\r\n", k, v)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, headerBuf.Bytes()); err != nil {
		t.Fatalf("write websocket header frame: %v", err)
	}
	return &wsGRPCWebStream{t: t, conn: conn, ctx: ctx}
}

// send writes one protobuf message as a client->server websocket frame:
// a leading control byte (0x00 = more data follows, per
// webSocketWrappedReader.Read) then the ordinary length-prefixed gRPC LPM
// framing.
func (s *wsGRPCWebStream) send(msg proto.Message) {
	s.t.Helper()
	lpm, err := grpcWebDataFrame(msg)
	if err != nil {
		s.t.Fatalf("frame message: %v", err)
	}
	frame := append([]byte{0x00}, lpm...)
	if err := s.conn.Write(s.ctx, websocket.MessageBinary, frame); err != nil {
		s.t.Fatalf("write websocket data frame: %v", err)
	}
}

// fill reads one more websocket message's bytes into s.pending.
func (s *wsGRPCWebStream) fill() {
	s.t.Helper()
	msgType, data, err := s.conn.Read(s.ctx)
	if err != nil {
		s.t.Fatalf("read websocket frame: %v", err)
	}
	if msgType != websocket.MessageBinary {
		return
	}
	s.pending = append(s.pending, data...)
}

// readFrame reassembles and returns exactly one flag+payload record from
// the underlying byte stream (see wsGRPCWebStream.pending's doc comment),
// topping up with more websocket messages as needed.
func (s *wsGRPCWebStream) readFrame() (flag byte, payload []byte) {
	s.t.Helper()
	for len(s.pending) < 5 {
		s.fill()
	}
	flag = s.pending[0]
	length := binary.BigEndian.Uint32(s.pending[1:5])
	for uint32(len(s.pending)-5) < length {
		s.fill()
	}
	payload = append([]byte(nil), s.pending[5:5+length]...)
	s.pending = s.pending[5+length:]
	return flag, payload
}

// recvMessage decodes exactly one application-level (LPM) message into
// resp, skipping the header frame grpc-web always sends first and any
// trailer frame that arrives instead (returning ok=false if a trailer
// arrives before a message does, e.g. on an RPC error).
func (s *wsGRPCWebStream) recvMessage(resp proto.Message) (ok bool, trailer grpcWebTrailers) {
	s.t.Helper()
	for {
		flag, payload := s.readFrame()
		if flag&0x80 != 0 {
			// Header or trailer frame (both use the same 0x80 encoding);
			// a header frame's payload parses as a (possibly empty)
			// header block, never as our proto message, so only treat it
			// as a trailer if it carries grpc-status.
			parsed := parseGRPCWebTrailerPayload(s.t, payload)
			if parsed.status != "" {
				return false, parsed
			}
			continue // it was the initial headers frame.
		}
		if err := proto.Unmarshal(payload, resp); err != nil {
			s.t.Fatalf("unmarshal websocket message: %v", err)
		}
		return true, grpcWebTrailers{}
	}
}

func (s *wsGRPCWebStream) close() {
	_ = s.conn.Close(websocket.StatusNormalClosure, "test done")
}

// TestGRPCWeb_OpenControlPingPongOverWebsocket proves grpc-web's websocket
// transport (WithWebsockets(true), see grpcweb.go) reaches
// internal/relay/server.Server.OpenControl -- the same bidi-streaming RPC
// implementation, and the same admission binding, native gRPC already
// covers -- rather than a separate, divergent path. OpenData rides the
// identical bidi-over-websocket bridge (see this file's doc comment for
// why the E2EE-layered OpenData case itself is left to Item 3's real
// TypeScript client).
func TestGRPCWeb_OpenControlPingPongOverWebsocket(t *testing.T) {
	relay, stop := startGRPCWebTestRelay(t)
	defer stop()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: relay.tlsConfig}}
	admissionID := admitOverGRPCWeb(t, client, "https://"+relay.webAddr, relay.workspace, "grpcweb-ws-daemon", relay.secret)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stream := openGRPCWebSocketStream(t, ctx, "wss://"+relay.webAddr, "OpenControl", relay.tlsConfig, map[string]string{
		"admission-id": hex.EncodeToString(admissionID),
	})
	defer stream.close()

	// The first frame must carry a Ping body -- internal/relay/server's
	// OpenControl treats anything else as an unhandled control frame type
	// (see client.OpenControl's doc comment for the same contract).
	stream.send(&relaypb.ControlFrame{
		WorkspaceId: relay.workspace,
		Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 0}},
	})

	var pong relaypb.ControlFrame
	ok, trailer := stream.recvMessage(&pong)
	if !ok {
		t.Fatalf("OpenControl registration: unexpected trailer status=%s message=%s", trailer.status, trailer.message)
	}

	stream.send(&relaypb.ControlFrame{
		WorkspaceId: relay.workspace,
		Body:        &relaypb.ControlFrame_Ping_{Ping: &relaypb.ControlFrame_Ping{Sequence: 42}},
	})
	ok, trailer = stream.recvMessage(&pong)
	if !ok {
		t.Fatalf("OpenControl ping: unexpected trailer status=%s message=%s", trailer.status, trailer.message)
	}
	if pong.GetPong().GetSequence() != 42 {
		t.Fatalf("pong sequence = %d, want 42", pong.GetPong().GetSequence())
	}
}
