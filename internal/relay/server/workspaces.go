package server

// workspaces.go — persisted relay-host enrollment for the relay: one JSON
// file mapping relay host id -> secret hash (ADR-0011: the raw secret is
// printed once at creation and never stored). Kept deliberately file-based
// and small; the relay is a self-hosted single-process service. The file is
// still named workspaces.json (its historical name; stored data is not
// renamed — ADR-0031 D12), and its keys are relay host ids.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/relay/admission"

	"github.com/spacingmind/smind/internal/securefile"
)

type hostRecord struct {
	SecretHash []byte `json:"secret_hash"`
}

type hostStore struct {
	path string
}

func newHostStore(dir string) hostStore {
	return hostStore{path: filepath.Join(dir, "workspaces.json")}
}

// load reads all enrolled relay hosts. A missing file is an empty store.
func (s hostStore) load() (map[string]hostRecord, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]hostRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("relay hosts: read: %w", err)
	}
	var m map[string]hostRecord
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("relay hosts: parse %s: %w", s.path, err)
	}
	return m, nil
}

// saveLocked writes the store back atomically (temp + rename).
func (s hostStore) save(records map[string]hostRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("relay hosts: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("relay hosts: mkdir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := securefile.WriteFile(tmp, data); err != nil {
		return fmt.Errorf("relay hosts: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("relay hosts: rename: %w", err)
	}
	return nil
}

// enroll creates a new relay host with a fresh 256-bit secret and returns
// the raw secret exactly once (never retrievable again).
func (s hostStore) enroll(id string) (secret []byte, err error) {
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	if _, exists := records[id]; exists {
		return nil, fmt.Errorf("relay hosts: %q already enrolled", id)
	}
	host, secret, err := admission.NewWorkspace(id)
	if err != nil {
		return nil, err
	}
	records[id] = hostRecord{SecretHash: host.SecretHash}
	if err := s.save(records); err != nil {
		return nil, err
	}
	return secret, nil
}

// verifier returns an admission.Verifier seeded with every enrolled
// relay host, for the running relay server.
func (s hostStore) verifier() (*admission.Verifier, error) {
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	v := admission.NewVerifier()
	for id, rec := range records {
		v.Register(admission.Workspace{ID: id, SecretHash: rec.SecretHash})
	}
	return v, nil
}

// list returns the enrolled relay host ids (sorted by the caller).
func (s hostStore) list() ([]string, error) {
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	return ids, nil
}

// remove deletes a relay host; its secret stops admitting once the relay
// restarts (the verifier is seeded from the store at startup).
func (s hostStore) remove(id string) error {
	records, err := s.load()
	if err != nil {
		return err
	}
	if _, exists := records[id]; !exists {
		return fmt.Errorf("relay hosts: %q is not enrolled", id)
	}
	delete(records, id)
	return s.save(records)
}

// rotate replaces a relay host's secret with a fresh 256-bit one and returns
// the new raw secret exactly once. The old secret no longer admits once the
// relay restarts.
func (s hostStore) rotate(id string) (secret []byte, err error) {
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	if _, exists := records[id]; !exists {
		return nil, fmt.Errorf("relay hosts: %q is not enrolled", id)
	}
	host, secret, err := admission.NewWorkspace(id)
	if err != nil {
		return nil, err
	}
	records[id] = hostRecord{SecretHash: host.SecretHash}
	if err := s.save(records); err != nil {
		return nil, err
	}
	return secret, nil
}
