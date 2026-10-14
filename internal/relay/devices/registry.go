package devices

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/securefile"
)

const (
	registryVersion = 1
	// DefaultWindowDuration and DefaultWindowSlots apply when OpenWindow is
	// given non-positive arguments.
	DefaultWindowDuration = 10 * time.Minute
	DefaultWindowSlots    = 1

	// lastSeenPersistEvery bounds how often a plain "seen again" touch is
	// written: every handshake would otherwise rewrite the file.
	lastSeenPersistEvery = 60 * time.Second
	minPrefixLen         = 4
)

// State is a device's admission state.
type State string

const (
	StateApproved State = "approved"
	StatePending  State = "pending"
	StateRevoked  State = "revoked"
)

// Device is one known device. Pub is the Ed25519 public key; ID is derived
// from it (DeviceID).
type Device struct {
	ID        string    `json:"device_id"`
	Pub       []byte    `json:"device_pub"`
	Label     string    `json:"label"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	State     State     `json:"state"`
}

// Reason says why a device was not admitted.
type Reason string

const (
	ReasonNone             Reason = ""
	ReasonRevoked          Reason = "revoked"
	ReasonAwaitingApproval Reason = "awaiting_approval"
)

// Decision is the outcome of Admit.
type Decision struct {
	Device   Device
	Approved bool
	Reason   Reason
	// New is true the first time this id was ever seen.
	New bool
}

// Window is a pairing window: while open and unexpired, the next Remaining
// unknown devices are approved automatically.
type Window struct {
	ExpiresAt time.Time `json:"expires_at"`
	Remaining int       `json:"remaining"`
	// OpenedAt is when the window opened. Only a device first seen at or after
	// it may take a slot: one that was already waiting (pending) before the
	// owner printed the offer is not the device the owner is pairing, and an
	// attacker who registered early must not be first in line.
	OpenedAt time.Time `json:"opened_at,omitempty"`
}

var (
	ErrNotFound  = errors.New("devices: no such device")
	ErrAmbiguous = errors.New("devices: device id prefix is ambiguous")
	// ErrTooManyPending refuses a new unknown device while MaxPending devices
	// already wait for approval, so a flood of fresh keys cannot grow (and
	// rewrite) the registry without bound or bury the real pending device.
	ErrTooManyPending = errors.New("devices: too many devices awaiting approval")
)

// MaxPending bounds devices in the pending state.
const MaxPending = 128

type fileFormat struct {
	V       int      `json:"v"`
	Devices []Device `json:"devices"`
	Window  *Window  `json:"window,omitempty"`
}

// snapshot is the registry's whole state; mutations work on a clone and only
// replace the live one once the file write succeeded.
type snapshot struct {
	devices map[string]Device
	window  *Window
}

func (s snapshot) clone() snapshot {
	c := snapshot{devices: make(map[string]Device, len(s.devices))}
	for k, v := range s.devices {
		c.devices[k] = v // Pub is never mutated in place, so sharing it is safe
	}
	if s.window != nil {
		w := *s.window
		c.window = &w
	}
	return c
}

// Registry is the file-backed device registry. All methods are safe for
// concurrent use.
type Registry struct {
	path string
	now  func() time.Time

	mu   sync.Mutex
	cur  snapshot
	disk map[string]time.Time // LastSeen value last written, per device
}

// Option configures Open.
type Option func(*Registry)

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) Option {
	return func(r *Registry) { r.now = now }
}

// Open loads the registry at path. A missing file is an empty registry; an
// unreadable, corrupt or unknown-version file is an error, never a silent
// reset — that would quietly un-revoke devices.
func Open(path string, opts ...Option) (*Registry, error) {
	r := &Registry{path: path, now: time.Now}
	for _, o := range opts {
		o(r)
	}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-reads the file, replacing the in-memory state. A CLI that edited
// the file while no daemon was running uses this to pick the edit up.
func (r *Registry) Reload() error {
	s, err := readFile(r.path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cur = s
	r.noteWritten(s)
	return nil
}

func readFile(path string) (snapshot, error) {
	s := snapshot{devices: map[string]Device{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("devices: read registry: %w", err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return s, fmt.Errorf("devices: parse registry %s: %w", path, err)
	}
	if f.V != registryVersion {
		return s, fmt.Errorf("devices: registry %s has unsupported version %d", path, f.V)
	}
	for _, d := range f.Devices {
		switch d.State {
		case StateApproved, StatePending, StateRevoked:
		default:
			return s, fmt.Errorf("devices: registry %s: device %q has unknown state %q", path, d.ID, d.State)
		}
		if len(d.Pub) != ed25519.PublicKeySize || DeviceID(d.Pub) != d.ID {
			return s, fmt.Errorf("devices: registry %s: device %q does not match its public key", path, d.ID)
		}
		if _, dup := s.devices[d.ID]; dup {
			return s, fmt.Errorf("devices: registry %s: duplicate device %q", path, d.ID)
		}
		s.devices[d.ID] = d
	}
	if f.Window != nil {
		w := *f.Window
		s.window = &w
	}
	return s, nil
}

// noteWritten records which LastSeen values the file now holds. Callers hold mu.
func (r *Registry) noteWritten(s snapshot) {
	r.disk = make(map[string]time.Time, len(s.devices))
	for id, d := range s.devices {
		r.disk[id] = d.LastSeen
	}
}

// write persists s atomically: a temp file in the same directory is written,
// restricted, fsynced and renamed over the target, so a crash or a failed
// write leaves the previous file intact. securefile.WriteFile alone would
// truncate in place, which is not crash-safe. The temp file is restricted
// before any data goes in so the contents are never readable by others.
func (r *Registry) write(s snapshot) error {
	f := fileFormat{V: registryVersion, Devices: make([]Device, 0, len(s.devices)), Window: s.window}
	for _, d := range s.devices {
		f.Devices = append(f.Devices, d)
	}
	sort.Slice(f.Devices, func(i, j int) bool { return deviceLess(f.Devices[i], f.Devices[j]) })
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("devices: write registry: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".relay-devices-*.tmp")
	if err != nil {
		return fmt.Errorf("devices: write registry: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("devices: write registry: %w", err)
	}
	if err := securefile.Restrict(tmpName); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("devices: write registry: %w", err)
	}
	if err := renameWithRetry(tmpName, r.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("devices: write registry: %w", err)
	}
	syncDir(dir)
	return nil
}

// renameWithRetry renames over the target, retrying briefly: on Windows the
// rename fails while another process (the CLI reading the file) holds it open.
func renameWithRetry(from, to string) error {
	var err error
	for i := 0; i < 5; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 10 * time.Millisecond)
	}
	return err
}

// syncDir flushes the directory entry so a crash right after a revoke cannot
// resurrect the previous file. Best effort: not every platform supports it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// commit persists next and, only on success, makes it the live state.
func (r *Registry) commit(next snapshot) error {
	if err := r.write(next); err != nil {
		return err
	}
	r.cur = next
	r.noteWritten(next)
	return nil
}

func deviceLess(a, b Device) bool {
	if !a.FirstSeen.Equal(b.FirstSeen) {
		return a.FirstSeen.Before(b.FirstSeen)
	}
	return a.ID < b.ID
}

func (d Device) copy() Device {
	d.Pub = append([]byte(nil), d.Pub...)
	return d
}

// expireLocked drops an expired window. The file is updated best-effort: the
// window is dead either way, and any later successful write persists the
// removal. Callers hold mu.
func (r *Registry) expireLocked() {
	if r.cur.window == nil || r.now().Before(r.cur.window.ExpiresAt) {
		return
	}
	next := r.cur.clone()
	next.window = nil
	if err := r.write(next); err == nil {
		r.noteWritten(next)
	}
	r.cur.window = nil
}

// List returns all devices sorted by first-seen time, then id.
func (r *Registry) List() []Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Device, 0, len(r.cur.devices))
	for _, d := range r.cur.devices {
		out = append(out, d.copy())
	}
	sort.Slice(out, func(i, j int) bool { return deviceLess(out[i], out[j]) })
	return out
}

// Get returns a copy of the device with the exact id.
func (r *Registry) Get(id string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.cur.devices[id]
	return d.copy(), ok
}

// Admit decides whether a device that proved possession of pub (see
// Hello.Verify) may connect, recording it if it is new. The decision and any
// state change are persisted before returning; on an I/O error the in-memory
// state is left as it was.
func (r *Registry) Admit(id string, pub []byte, label string) (Decision, error) {
	if len(pub) != ed25519.PublicKeySize || DeviceID(pub) != id {
		return Decision{}, ErrIDMismatch
	}
	if err := ValidateLabel(label); err != nil {
		return Decision{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
	now := r.now().UTC()

	next := r.cur.clone()
	dev, known := next.devices[id]
	changed := false // a state/label/window change that must hit the disk
	isNew := false
	if !known {
		pending := 0
		for _, d := range next.devices {
			if d.State == StatePending {
				pending++
			}
		}
		if pending >= MaxPending {
			return Decision{}, ErrTooManyPending
		}
		dev = Device{ID: id, Pub: append([]byte(nil), pub...), Label: label, FirstSeen: now, State: StatePending}
		isNew = true
		changed = true
	} else if dev.Label == "" && label != "" {
		dev.Label = label // a rename sticks; only an empty label is filled
		changed = true
	}
	dev.LastSeen = now

	// A device that is not yet approved takes a pairing slot if one is open and
	// it was first seen after the window opened (see Window.OpenedAt).
	if dev.State == StatePending && next.window != nil && next.window.Remaining > 0 &&
		!dev.FirstSeen.Before(next.window.OpenedAt) {
		dev.State = StateApproved
		next.window.Remaining--
		if next.window.Remaining <= 0 {
			next.window = nil
		}
		changed = true
	}
	next.devices[id] = dev

	if !changed && now.Sub(r.disk[id]) >= lastSeenPersistEvery {
		changed = true
	}
	if changed {
		if err := r.commit(next); err != nil {
			return Decision{}, err
		}
	} else {
		r.cur = next
	}

	dec := Decision{Device: dev.copy(), New: isNew}
	switch dev.State {
	case StateApproved:
		dec.Approved = true
	case StateRevoked:
		dec.Reason = ReasonRevoked
	default:
		dec.Reason = ReasonAwaitingApproval
	}
	return dec, nil
}

func (r *Registry) setState(id string, from func(State) bool, to State) (Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
	dev, ok := r.cur.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	if dev.State == to || !from(dev.State) {
		return dev.copy(), nil
	}
	next := r.cur.clone()
	dev.State = to
	next.devices[id] = dev
	if err := r.commit(next); err != nil {
		return Device{}, err
	}
	return dev.copy(), nil
}

// Approve is the explicit human decision that lets a pending or revoked
// device in. Approving an already-approved device is a no-op.
func (r *Registry) Approve(id string) (Device, error) {
	return r.setState(id, func(s State) bool { return s == StatePending || s == StateRevoked }, StateApproved)
}

// Revoke refuses a device from now on; its key and label are kept so the
// refusal can be explained and the decision later reversed with Approve.
func (r *Registry) Revoke(id string) (Device, error) {
	return r.setState(id, func(State) bool { return true }, StateRevoked)
}

// Rename sets a device's label, with the same rules as a hello label.
func (r *Registry) Rename(id, label string) (Device, error) {
	if err := ValidateLabel(label); err != nil {
		return Device{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	dev, ok := r.cur.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	next := r.cur.clone()
	dev.Label = label
	next.devices[id] = dev
	if err := r.commit(next); err != nil {
		return Device{}, err
	}
	return dev.copy(), nil
}

// OpenWindow opens (replacing any open one) a pairing window that approves
// the next maxNew unknown devices within d. d<=0 means 10 minutes and
// maxNew<=0 means 1.
func (r *Registry) OpenWindow(d time.Duration, maxNew int) error {
	if d <= 0 {
		d = DefaultWindowDuration
	}
	if maxNew <= 0 {
		maxNew = DefaultWindowSlots
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.cur.clone()
	next.window = &Window{ExpiresAt: r.now().UTC().Add(d), Remaining: maxNew, OpenedAt: r.now().UTC()}
	return r.commit(next)
}

// Window reports the open pairing window; ok is false when none is open or
// it has expired.
func (r *Registry) Window() (Window, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
	if r.cur.window == nil {
		return Window{}, false
	}
	return *r.cur.window, true
}

// CloseWindow closes any open pairing window.
func (r *Registry) CloseWindow() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur.window == nil {
		return nil
	}
	next := r.cur.clone()
	next.window = nil
	return r.commit(next)
}

// Resolve maps an exact id or a case-insensitive unique prefix (at least 4
// characters) to a full device id.
func (r *Registry) Resolve(idOrPrefix string) (string, error) {
	q := strings.ToLower(idOrPrefix)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cur.devices[q]; ok {
		return q, nil
	}
	if len(q) < minPrefixLen {
		return "", ErrNotFound
	}
	var match string
	n := 0
	for id := range r.cur.devices {
		if strings.HasPrefix(id, q) {
			match = id
			n++
		}
	}
	switch n {
	case 0:
		return "", ErrNotFound
	case 1:
		return match, nil
	}
	return "", ErrAmbiguous
}
