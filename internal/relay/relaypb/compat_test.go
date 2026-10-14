package relaypb

// compat_test.go — ADR-0031 D9 wire compatibility. The multi-device
// additions are additive: a message written with the current definition must
// still decode under the previous one (older peers ignore the new fields),
// and a message written by an older peer must decode unchanged under the
// current one. testdata/relay_v0.desc is the descriptor set of relay.proto
// as it was before the additions (`protoc --descriptor_set_out`), loaded
// dynamically so the test exercises a real previous-revision decoder.

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func previousFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	raw, err := os.ReadFile("testdata/relay_v0.desc")
	if err != nil {
		t.Fatalf("read previous descriptor: %v", err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		t.Fatalf("parse previous descriptor: %v", err)
	}
	fd, err := protodesc.NewFile(set.GetFile()[0], &protoregistry.Files{})
	if err != nil {
		t.Fatalf("build previous descriptor: %v", err)
	}
	return fd
}

// goldenKeepaliveFrame is Frame{workspace_id:"ws", session_id:"s1",
// device_id:"d1", direction:DEVICE_TO_DAEMON, keepalive:true} as encoded by
// the current definition (field 7 = 0x38 0x01 at the end).
const goldenKeepaliveFrame = "0a027773120273311a026431200238" + "01"

// goldenSyncFrame is ControlFrame{workspace_id:"ws", sync:{devices:[{d1,s1}]}}.
const goldenSyncFrame = "0a02" + "7773" + "320a" + "0a08" + "0a026431" + "12027331"

func TestGoldenBytesCurrentEncoding(t *testing.T) {
	frame, err := proto.MarshalOptions{Deterministic: true}.Marshal(&Frame{
		WorkspaceId: "ws", SessionId: []byte("s1"), DeviceId: "d1",
		Direction: Direction_DIRECTION_DEVICE_TO_DAEMON, Keepalive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(frame); got != goldenKeepaliveFrame {
		t.Fatalf("keepalive Frame bytes changed:\n got %s\nwant %s", got, goldenKeepaliveFrame)
	}
	cf, err := proto.MarshalOptions{Deterministic: true}.Marshal(&ControlFrame{
		WorkspaceId: "ws",
		Body: &ControlFrame_Sync_{Sync: &ControlFrame_Sync{Devices: []*ControlFrame_DeviceRef{
			{DeviceId: "d1", SessionId: []byte("s1")},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(cf); got != goldenSyncFrame {
		t.Fatalf("Sync ControlFrame bytes changed:\n got %s\nwant %s", got, goldenSyncFrame)
	}
}

// TestNewFrameDecodesUnderPreviousDefinition: the keepalive flag is unknown
// to the previous Frame; every field the previous definition knows survives.
func TestNewFrameDecodesUnderPreviousDefinition(t *testing.T) {
	prev := previousFile(t)
	raw, _ := hex.DecodeString(goldenKeepaliveFrame)
	msg := dynamicpb.NewMessage(prev.Messages().ByName("Frame"))
	if err := proto.Unmarshal(raw, msg); err != nil {
		t.Fatalf("previous definition rejects a new Frame: %v", err)
	}
	fields := msg.Descriptor().Fields()
	if got := msg.Get(fields.ByName("workspace_id")).String(); got != "ws" {
		t.Fatalf("workspace_id = %q", got)
	}
	if got := msg.Get(fields.ByName("device_id")).String(); got != "d1" {
		t.Fatalf("device_id = %q", got)
	}
	if got := msg.Get(fields.ByName("session_id")).Bytes(); !bytes.Equal(got, []byte("s1")) {
		t.Fatalf("session_id = %q", got)
	}
	if got := msg.Get(fields.ByName("direction")).Enum(); got != 2 {
		t.Fatalf("direction = %d", got)
	}
	if len(msg.GetUnknown()) == 0 {
		t.Fatal("keepalive should land in unknown fields of the previous definition")
	}
}

// TestNewControlFramesDecodeUnderPreviousDefinition: a Sync body (new oneof
// member) leaves the previous ControlFrame with its workspace id and no body;
// a DeviceAttach with the new session_id still decodes with device_id.
func TestNewControlFramesDecodeUnderPreviousDefinition(t *testing.T) {
	prev := previousFile(t)
	cfDesc := prev.Messages().ByName("ControlFrame")

	raw, _ := hex.DecodeString(goldenSyncFrame)
	msg := dynamicpb.NewMessage(cfDesc)
	if err := proto.Unmarshal(raw, msg); err != nil {
		t.Fatalf("previous definition rejects a Sync ControlFrame: %v", err)
	}
	if got := msg.Get(cfDesc.Fields().ByName("workspace_id")).String(); got != "ws" {
		t.Fatalf("workspace_id = %q", got)
	}
	if od := cfDesc.Oneofs().ByName("body"); msg.WhichOneof(od) != nil {
		t.Fatalf("previous definition should see no body, got %v", msg.WhichOneof(od).Name())
	}

	attach, err := proto.Marshal(&ControlFrame{
		WorkspaceId: "ws",
		Body: &ControlFrame_DeviceAttach_{DeviceAttach: &ControlFrame_DeviceAttach{
			DeviceId: "d1", SessionId: []byte("s1"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg = dynamicpb.NewMessage(cfDesc)
	if err := proto.Unmarshal(attach, msg); err != nil {
		t.Fatalf("previous definition rejects a DeviceAttach with session_id: %v", err)
	}
	od := cfDesc.Oneofs().ByName("body")
	fd := msg.WhichOneof(od)
	if fd == nil || fd.Name() != "device_attach" {
		t.Fatalf("body = %v, want device_attach", fd)
	}
	inner := msg.Get(fd).Message()
	if got := inner.Get(fd.Message().Fields().ByName("device_id")).String(); got != "d1" {
		t.Fatalf("device_id = %q", got)
	}
}

// TestPreviousEncodingDecodesUnderCurrentDefinition: bytes written by an
// old peer (no new fields) decode under the current definition with the new
// fields at their zero values.
func TestPreviousEncodingDecodesUnderCurrentDefinition(t *testing.T) {
	prev := previousFile(t)
	frameDesc := prev.Messages().ByName("Frame")
	old := dynamicpb.NewMessage(frameDesc)
	fs := frameDesc.Fields()
	old.Set(fs.ByName("workspace_id"), protoreflect.ValueOfString("ws"))
	old.Set(fs.ByName("session_id"), protoreflect.ValueOfBytes([]byte("s1")))
	old.Set(fs.ByName("device_id"), protoreflect.ValueOfString("d1"))
	old.Set(fs.ByName("payload"), protoreflect.ValueOfBytes([]byte("ct")))
	raw, err := proto.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var cur Frame
	if err := proto.Unmarshal(raw, &cur); err != nil {
		t.Fatal(err)
	}
	if cur.GetKeepalive() || cur.GetDeviceId() != "d1" || string(cur.GetPayload()) != "ct" {
		t.Fatalf("old Frame decoded wrong: %+v", &cur)
	}

	ctl := dynamicpb.NewMessage(prev.Messages().ByName("ControlFrame"))
	ctl.Set(ctl.Descriptor().Fields().ByName("workspace_id"), protoreflect.ValueOfString("ws"))
	raw, _ = proto.Marshal(ctl)
	var cf ControlFrame
	if err := proto.Unmarshal(raw, &cf); err != nil {
		t.Fatal(err)
	}
	if cf.GetBody() != nil || cf.GetWorkspaceId() != "ws" {
		t.Fatalf("old ControlFrame decoded wrong: %+v", &cf)
	}
}
