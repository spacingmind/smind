package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spacingmind/smind/internal/securefile"
)

// SecretsFile is the name of the per-host credentials file the fleet keeps
// in the smind home directory, next to the auth token.
const SecretsFile = "fleet-secrets"

// ErrInsecure is returned instead of reading a fleet-secrets file whose
// permissions are looser than securefile leaves them: a world-readable
// credentials file is treated as compromised, not quietly tightened, so a
// human decides what to do.
var ErrInsecure = errors.New("link: fleet-secrets file is not private; fix its permissions or remove it")

// Entry is one paired host's credentials. Its String and GoString hide the
// values, so a stray %v, %+v or %#v in a log line cannot leak them.
type Entry struct {
	// Password authenticates the local human to the peer host.
	Password string `json:"password,omitempty"`
	// RelaySecret is the shared secret guarding the relayed link to the host.
	RelaySecret string `json:"relaySecret,omitempty"`
}

// String implements fmt.Stringer with the values redacted.
func (e Entry) String() string { return "link.Entry{redacted}" }

// GoString implements fmt.GoStringer with the values redacted.
func (e Entry) GoString() string { return "link.Entry{redacted}" }

// Secrets is the fleet-secrets file under one home directory. All reads and
// writes serialize on its mutex; the file is only created by Put.
type Secrets struct {
	mu   sync.Mutex
	path string
}

// OpenSecrets returns the secrets file for home. Nothing is read or written
// yet: a home without a fleet never grows the file.
func OpenSecrets(home string) *Secrets {
	return &Secrets{path: filepath.Join(home, SecretsFile)}
}

// Get returns hostID's entry and whether the host is known.
func (s *Secrets) Get(hostID string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return Entry{}, false, err
	}
	e, ok := m[hostID]
	return e, ok, nil
}

// Put stores hostID's entry, preserving every other host's, and writes the
// file back restricted to the current user (securefile.WriteFile).
func (s *Secrets) Put(hostID string, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	m[hostID] = e
	return s.save(m)
}

// Delete drops hostID's entry. Removing the last one leaves an empty "{}"
// file rather than deleting it, so ownership of the path never flips.
func (s *Secrets) Delete(hostID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	delete(m, hostID)
	return s.save(m)
}

// load reads the file's entries. A missing file is an empty map; a file that
// is not private is refused before its bytes are looked at.
func (s *Secrets) load() (map[string]Entry, error) {
	if _, err := os.Stat(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]Entry{}, nil
		}
		return nil, err
	}
	private, err := securefile.IsPrivate(s.path)
	if err != nil {
		return nil, err
	}
	if !private {
		return nil, ErrInsecure
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	m := map[string]Entry{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("link: parse %s: %w", s.path, err)
	}
	return m, nil
}

// save writes the entries back with securefile's restrictions.
func (s *Secrets) save(m map[string]Entry) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return securefile.WriteFile(s.path, append(data, '\n'))
}

// Redact renders s as "[redacted]", keeping "" for unset so callers can
// still distinguish "no credential" from "a credential exists".
func Redact(s string) string {
	if s == "" {
		return ""
	}
	return "[redacted]"
}
