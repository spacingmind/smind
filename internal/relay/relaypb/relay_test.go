package relaypb

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestFrameRoundTrip proves the generated code compiles and that a Frame —
// the relay's core envelope — survives a proto marshal/unmarshal cycle with
// all routing metadata and the opaque ciphertext payload intact.
func TestFrameRoundTrip(t *testing.T) {
	in := &Frame{
		WorkspaceId: "ws-123",
		SessionId:   []byte("session-abc"),
		DeviceId:    "device-1",
		Direction:   Direction_DIRECTION_DAEMON_TO_DEVICE,
		Sequence:    42,
		Payload:     []byte("opaque ciphertext"),
	}

	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Frame
	if err := proto.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !proto.Equal(in, &out) {
		t.Fatalf("round-trip mismatch:\nin:  %v\nout: %v", &in, &out)
	}
	if !bytes.Equal(in.Payload, out.Payload) {
		t.Fatalf("payload changed: %q != %q", in.Payload, out.Payload)
	}
}

// TestAdmitRoundTrip checks the admission transcript messages round-trip —
// the HMAC challenge-response inputs must all survive encoding exactly, or
// the daemon's proof would not verify on the relay side.
func TestAdmitRoundTrip(t *testing.T) {
	chalReq := &AdmitChallengeRequest{
		ProtocolVersion: 1,
		WorkspaceId:     "ws-123",
		ClientNonce:     []byte("client-nonce"),
		DaemonKeyId:     "daemon-key-1",
	}
	chal := &AdmitChallengeResponse{ServerNonce: []byte("server-nonce")}
	admit := &AdmitRequest{
		ProtocolVersion: chalReq.ProtocolVersion,
		WorkspaceId:     chalReq.WorkspaceId,
		ClientNonce:     chalReq.ClientNonce,
		DaemonKeyId:     chalReq.DaemonKeyId,
		ServerNonce:     chal.ServerNonce,
		Hmac:            []byte("hmac-tag"),
	}
	resp := &AdmitResponse{AdmissionId: []byte("admission-1")}

	for _, m := range []proto.Message{chalReq, chal, admit, resp} {
		data, err := proto.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %T: %v", m, err)
		}
		out := m.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(data, out); err != nil {
			t.Fatalf("unmarshal %T: %v", m, err)
		}
		if !proto.Equal(m, out) {
			t.Fatalf("%T round-trip mismatch:\nin:  %+v\nout: %+v", m, m, out)
		}
	}
}
