package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/securefile"
)

const registryVersion = 1

// ErrCorrupt marks a registry file that could not be read, parsed or
// understood. Opening one is an error, never a silent reset: the file holds
// the only copy of each device's push key.
var ErrCorrupt = errors.New("push: registry file is corrupt or has an unknown version")

// Registration is one device's push registration: the push token and the
// per-device sealing key (K_push, ADR-0040 §1) the device sent over the E2EE
// channel. There is at most one per device id, and a token belongs to at most
// one device id.
type Registration struct {
	DeviceID     string    `json:"device_id"`
	Provider     string    `json:"provider"` // "expo"
	Token        string    `json:"token"`
	Platform     string    `json:"platform"` // "android" | "ios"
	Key          []byte    `json:"key"`      // 32 bytes, std base64 in JSON (encoding/json does this for []byte)
	Kinds        []string  `json:"kinds"`
	RegisteredAt time.Time `json:"registered_at"`
}

type fileFormat struct {
	V             int            `json:"v"`
	Registrations []Registration `json:"registrations"`
}

// snapshot is the registry's whole state; mutations work on a clone and only
// replace the live one once the file write succeeded.
type snapshot struct {
	regs map[string]Registration
}

func (s snapshot) clone() snapshot {
	c := snapshot{regs: make(map[string]Registration, len(s.regs))}
	for k, v := range s.regs {
		c.regs[k] = v.copy()
	}
	return c
}

func (r Registration) copy() Registration {
	r.Key = append([]byte(nil), r.Key...)
	r.Kinds = append([]string(nil), r.Kinds...)
	return r
}

// Registry is the file-backed store of push registrations. All methods are
// safe for concurrent use.
type Registry struct {
	path string

	mu  sync.Mutex
	cur snapshot
}

// OpenRegistry loads the registration file at path. A missing file is an
// empty registry; an unreadable, corrupt or unknown-version file is an error
// wrapping ErrCorrupt.
func OpenRegistry(path string) (*Registry, error) {
	s, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return &Registry{path: path, cur: s}, nil
}

func readFile(path string) (snapshot, error) {
	s := snapshot{regs: map[string]Registration{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("%w: read %s: %v", ErrCorrupt, path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return s, fmt.Errorf("%w: parse %s: %v", ErrCorrupt, path, err)
	}
	if f.V != registryVersion {
		return s, fmt.Errorf("%w: %s has version %d", ErrCorrupt, path, f.V)
	}
	for _, reg := range f.Registrations {
		s.regs[reg.DeviceID] = reg
	}
	return s, nil
}

// Put upserts reg by DeviceID. Token takeover: any OTHER registration holding
// the same token is deleted in the same write, so an old, revoked or
// abandoned device id can never share a live phone's token.
func (r *Registry) Put(reg Registration) error {
	reg = reg.copy()
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.cur.clone()
	for id, o := range next.regs {
		if o.Token == reg.Token && id != reg.DeviceID {
			delete(next.regs, id)
		}
	}
	next.regs[reg.DeviceID] = reg
	return r.commit(next)
}

// Get returns a copy of the registration for deviceID.
func (r *Registry) Get(deviceID string) (Registration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reg, ok := r.cur.regs[deviceID]
	return reg.copy(), ok
}

// Remove deletes the registration for deviceID, reporting false without
// writing when there is none.
func (r *Registry) Remove(deviceID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cur.regs[deviceID]; !ok {
		return false, nil
	}
	next := r.cur.clone()
	delete(next.regs, deviceID)
	if err := r.commit(next); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveToken deletes every registration holding token (Expo
// DeviceNotRegistered) and returns how many went; it does not write when
// there are none.
func (r *Registry) RemoveToken(token string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.cur.clone()
	n := 0
	for id, reg := range next.regs {
		if reg.Token == token {
			delete(next.regs, id)
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	if err := r.commit(next); err != nil {
		return 0, err
	}
	return n, nil
}

// List returns all registrations sorted by device id.
func (r *Registry) List() []Registration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Registration, 0, len(r.cur.regs))
	for _, reg := range r.cur.regs {
		out = append(out, reg.copy())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}

// Clear removes every registration (the hard reset of `smind relay host
// rotate`).
func (r *Registry) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cur.regs) == 0 {
		return nil
	}
	return r.commit(snapshot{regs: map[string]Registration{}})
}

// write persists s atomically: a temp file in the same directory is written,
// restricted, fsynced and renamed over the target, so a crash or a failed
// write leaves the previous file intact. securefile.WriteFile alone would
// truncate in place, which is not crash-safe. The temp file is restricted
// before any data goes in so the contents are never readable by others.
func (r *Registry) write(s snapshot) error {
	f := fileFormat{V: registryVersion, Registrations: make([]Registration, 0, len(s.regs))}
	for _, reg := range s.regs {
		f.Registrations = append(f.Registrations, reg)
	}
	sort.Slice(f.Registrations, func(i, j int) bool {
		return f.Registrations[i].DeviceID < f.Registrations[j].DeviceID
	})
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("push: write registry: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".push-registrations-*.tmp")
	if err != nil {
		return fmt.Errorf("push: write registry: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("push: write registry: %w", err)
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
		return fmt.Errorf("push: write registry: %w", err)
	}
	if err := renameWithRetry(tmpName, r.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("push: write registry: %w", err)
	}
	syncDir(dir)
	return nil
}

// commit persists next and, only on success, makes it the live state.
func (r *Registry) commit(next snapshot) error {
	if err := r.write(next); err != nil {
		return err
	}
	r.cur = next
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

// syncDir flushes the directory entry so a crash right after a write cannot
// resurrect the previous file. Best effort: not every platform supports it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// RegistryFileName is the registry's file under the daemon's home directory.
const RegistryFileName = "push-registrations.json"

// RegistryPath is the registry file under dir (the daemon's $SMIND_HOME).
func RegistryPath(dir string) string { return filepath.Join(dir, RegistryFileName) }
