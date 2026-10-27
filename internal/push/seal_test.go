package push

// Cross-language seal/open vector (ADR-0040 §1). The Go implementation
// generates testdata/push_seal_vector.json; the mobile client reads it.
//
//	go test ./internal/push -run TestWriteVector -update

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"
)

var update = flag.Bool("update", false, "rewrite testdata/push_seal_vector.json")

const vectorPath = "testdata/push_seal_vector.json"

type vectorNegative struct {
	Name     string `json:"name"`
	DeviceID string `json:"device_id,omitempty"`
	Blob     Blob   `json:"blob"`
	Error    string `json:"error"`
}

type vectorFile struct {
	Comment      string           `json:"comment"`
	KeyHex       string           `json:"key_hex"`
	DeviceID     string           `json:"device_id"`
	NonceHex     string           `json:"nonce_hex"`
	Event        Event            `json:"event"`
	PlaintextHex string           `json:"plaintext_hex"`
	Blob         Blob             `json:"blob"`
	Negative     []vectorNegative `json:"negative"`
}

func hx(b []byte) string { return hex.EncodeToString(b) }

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// flipB64 flips one bit of the i'th decoded byte of s and re-encodes it.
func flipB64(t *testing.T, s string, i int) string {
	t.Helper()
	raw := mustB64(t, s)
	raw[i] ^= 0x01
	return base64.RawURLEncoding.EncodeToString(raw)
}

// errorName maps an error to the sentinel name the vector file uses; the
// mobile tests map their own error types onto the same strings.
func errorName(err error) string {
	switch {
	case errors.Is(err, ErrKey):
		return "ErrKey"
	case errors.Is(err, ErrVersion):
		return "ErrVersion"
	case errors.Is(err, ErrMalformed):
		return "ErrMalformed"
	case errors.Is(err, ErrAuth):
		return "ErrAuth"
	case errors.Is(err, ErrTooLarge):
		return "ErrTooLarge"
	case err == nil:
		return "none"
	default:
		return "other: " + err.Error()
	}
}

func testKey() []byte {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

const testDeviceID = "device-a00000000000000000000000"

func testEvent() Event {
	return Event{
		ID:     "evt-42",
		Kind:   "permission",
		TaskID: 12,
		RunID:  "run-7",
		ChatID: 3,
		Title:  "Approve the push",
		Body:   "Run needs approval: git push origin smind/task-12",
		At:     1790000000,
	}
}

func sealDefault(t *testing.T) Blob {
	t.Helper()
	blob, err := Seal(testKey(), testDeviceID, testEvent())
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// wantError asserts err is exactly the sentinel and that no partial Event
// escaped.
func wantError(t *testing.T, ev Event, err error, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if ev != (Event{}) {
		t.Fatalf("Event = %+v, want the zero value", ev)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	ev := Event{
		ID:     "evt-99",
		Kind:   "needs_continue",
		TaskID: 77,
		RunID:  "run-9",
		ChatID: 5,
		Title:  "Turn cap reached",
		Body:   "The run stopped at the turn cap.",
		At:     1789991234,
	}
	blob, err := Seal(testKey(), testDeviceID, ev)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(testKey(), testDeviceID, blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != ev {
		t.Fatalf("Open = %+v, want %+v", got, ev)
	}
}

func TestSealPadsToBucket(t *testing.T) {
	for _, title := range []string{"a", "a slightly longer title", strings.Repeat("t", 70)} {
		blob, err := Seal(testKey(), testDeviceID, Event{Title: title, Body: "b"})
		if err != nil {
			t.Fatal(err)
		}
		if n := len(mustB64(t, blob.C)) - 16; n%bucketSize != 0 || n > MaxPlainSize {
			t.Fatalf("padded plaintext = %d bytes for title %q, want a multiple of %d <= %d", n, title, bucketSize, MaxPlainSize)
		}
	}

	long := strings.Repeat("é", 200)
	blob, err := Seal(testKey(), testDeviceID, Event{Title: long, Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(testKey(), testDeviceID, blob)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != string([]rune(long)[:MaxTitleRunes]) {
		t.Fatalf("title truncated to %d runes, want %d", utf8.RuneCountInString(got.Title), MaxTitleRunes)
	}
}

func TestSealTooLarge(t *testing.T) {
	ev := Event{
		ID:    strings.Repeat("é", 2000),
		Title: strings.Repeat("é", MaxTitleRunes),
		Body:  strings.Repeat("é", MaxBodyRunes),
	}
	_, err := Seal(testKey(), testDeviceID, ev)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
}

func TestOpenRejectsWrongDeviceID(t *testing.T) {
	ev, err := Open(testKey(), "other-device-id", sealDefault(t))
	wantError(t, ev, err, ErrAuth)
}

func TestOpenRejectsFlippedCiphertext(t *testing.T) {
	blob := sealDefault(t)
	blob.C = flipB64(t, blob.C, 0)
	ev, err := Open(testKey(), testDeviceID, blob)
	wantError(t, ev, err, ErrAuth)
}

func TestOpenRejectsFlippedNonce(t *testing.T) {
	blob := sealDefault(t)
	blob.N = flipB64(t, blob.N, 0)
	ev, err := Open(testKey(), testDeviceID, blob)
	wantError(t, ev, err, ErrAuth)
}

func TestOpenRejectsBadVersion(t *testing.T) {
	blob := sealDefault(t)
	blob.V = 2
	ev, err := Open(testKey(), testDeviceID, blob)
	wantError(t, ev, err, ErrVersion)
}

func TestOpenRejectsTruncated(t *testing.T) {
	blob := sealDefault(t)
	blob.C = base64.RawURLEncoding.EncodeToString(mustB64(t, blob.C)[:10])
	ev, err := Open(testKey(), testDeviceID, blob)
	wantError(t, ev, err, ErrMalformed)
}

func TestOpenRejectsWrongKey(t *testing.T) {
	key := testKey()
	key[0] ^= 0xff
	ev, err := Open(key, testDeviceID, sealDefault(t))
	wantError(t, ev, err, ErrAuth)
}

func TestSealUsesFreshNonce(t *testing.T) {
	a, err := Seal(testKey(), testDeviceID, testEvent())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Seal(testKey(), testDeviceID, testEvent())
	if err != nil {
		t.Fatal(err)
	}
	if a.N == b.N {
		t.Fatal("nonce reused across seals")
	}
	if a.C == b.C {
		t.Fatal("ciphertext reused across seals")
	}
}

const vectorDeviceID = "2mxq7vdbf5nxkzw4uj3ovp6kxe"

func buildVector(t *testing.T) *vectorFile {
	t.Helper()
	key := testKey() // 0x01..0x20
	nonce := make([]byte, chacha20poly1305.NonceSize)
	for i := range nonce {
		nonce[i] = byte(0xa0 + i)
	}
	ev := Event{
		ID:     "evt-0001",
		Kind:   "permission",
		TaskID: 12,
		RunID:  "run-7",
		ChatID: 3,
		Title:  "Fix the login redirect",
		Body:   "Run needs approval: git push origin smind/task-12",
		At:     1790000000,
	}
	blob, err := sealWithNonce(key, vectorDeviceID, ev, nonce)
	if err != nil {
		t.Fatal(err)
	}
	framed, err := frame(ev)
	if err != nil {
		t.Fatal(err)
	}
	flipped := blob
	flipped.C = flipB64(t, flipped.C, 0)
	badVersion := blob
	badVersion.V = 2
	truncated := blob
	truncated.C = base64.RawURLEncoding.EncodeToString(mustB64(t, blob.C)[:10])
	return &vectorFile{
		Comment: "Push seal/open vector (ADR-0040 section 1), generated by internal/push TestWriteVector -update. " +
			"blob is Seal(key_hex, device_id, event) under nonce_hex; plaintext_hex is the u16BE-length-prefixed, " +
			"zero-padded framed plaintext (the AEAD input). Each `negative` opens its `blob` (under `device_id` if set, " +
			"else the top-level device_id) with Open and must fail with the named error.",
		KeyHex:       hx(key),
		DeviceID:     vectorDeviceID,
		NonceHex:     hx(nonce),
		Event:        ev,
		PlaintextHex: hx(framed),
		Blob:         blob,
		Negative: []vectorNegative{
			{Name: "wrong_device_id", DeviceID: "otherdevice0000000000000000", Blob: blob, Error: "ErrAuth"},
			{Name: "flipped_ciphertext", Blob: flipped, Error: "ErrAuth"},
			{Name: "bad_version", Blob: badVersion, Error: "ErrVersion"},
			{Name: "truncated", Blob: truncated, Error: "ErrMalformed"},
		},
	}
}

// TestWriteVector regenerates the shared vector file with -update, and
// otherwise only checks that the committed file is present.
func TestWriteVector(t *testing.T) {
	if *update {
		data, err := json.MarshalIndent(buildVector(t), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(vectorPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := os.Stat(vectorPath); err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
}

// TestSealOpenVector: the committed vector reproduces byte-for-byte under
// Seal/Open, and every negative entry fails with its named error.
func TestSealOpenVector(t *testing.T) {
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	key, nonce := mustHex(t, v.KeyHex), mustHex(t, v.NonceHex)

	blob, err := sealWithNonce(key, v.DeviceID, v.Event, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if blob != v.Blob {
		t.Fatalf("sealWithNonce = %+v, want %+v", blob, v.Blob)
	}
	ev, err := Open(key, v.DeviceID, v.Blob)
	if err != nil {
		t.Fatal(err)
	}
	if ev != v.Event {
		t.Fatalf("Open = %+v, want %+v", ev, v.Event)
	}

	for _, n := range v.Negative {
		t.Run(n.Name, func(t *testing.T) {
			deviceID := v.DeviceID
			if n.DeviceID != "" {
				deviceID = n.DeviceID
			}
			ev, err := Open(key, deviceID, n.Blob)
			if got := errorName(err); got != n.Error {
				t.Fatalf("error = %s (%v), want %s", got, err, n.Error)
			}
			if ev != (Event{}) {
				t.Fatalf("Event = %+v, want the zero value", ev)
			}
		})
	}
}

// A task-less automation alert carries its automation id through seal/open;
// an ordinary event omits the field (the mobile vector stays unchanged).
func TestSealCarriesAutomationID(t *testing.T) {
	key := make([]byte, KeySize)
	ev := Event{ID: "e1", Kind: KindError, TaskID: 0, AutomationID: 3, Title: "Automation paused: nightly", Body: "Automation paused: x", At: 1}
	blob, err := Seal(key, "dev", ev)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(key, "dev", blob)
	if err != nil || got.AutomationID != 3 || got.TaskID != 0 {
		t.Fatalf("Open = %+v, %v", got, err)
	}
	plain, _ := json.Marshal(Event{ID: "e2", Kind: KindDone, TaskID: 1})
	if strings.Contains(string(plain), "automationId") {
		t.Fatalf("an ordinary event serialises automationId: %s", plain)
	}
}
