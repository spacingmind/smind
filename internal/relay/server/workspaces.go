package server

// workspaces.go — persisted workspace enrollment for the relay: one JSON
// file mapping workspace ID -> secret hash (ADR-0011: the raw secret is
// printed once at creation and never stored). Kept deliberately file-based
// and small; the relay is a self-hosted single-process service.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/relay/admission"
)

type workspaceRecord struct {
	SecretHash []byte `json:"secret_hash"`
}

type workspaceStore struct {
	path string
}

func newWorkspaceStore(dir string) workspaceStore {
	return workspaceStore{path: filepath.Join(dir, "workspaces.json")}
}

// load reads all enrolled workspaces. A missing file is an empty store.
func (s workspaceStore) load() (map[string]workspaceRecord, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]workspaceRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("relay workspaces: read: %w", err)
	}
	var m map[string]workspaceRecord
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("relay workspaces: parse %s: %w", s.path, err)
	}
	return m, nil
}

// saveLocked writes the store back atomically (temp + rename).
func (s workspaceStore) save(records map[string]workspaceRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("relay workspaces: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("relay workspaces: mkdir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("relay workspaces: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("relay workspaces: rename: %w", err)
	}
	return nil
}

// enroll creates a new workspace with a fresh 256-bit secret and returns
// the raw secret exactly once (never retrievable again).
func (s workspaceStore) enroll(id string) (secret []byte, err error) {
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	if _, exists := records[id]; exists {
		return nil, fmt.Errorf("relay workspaces: %q already enrolled", id)
	}
	ws, secret, err := admission.NewWorkspace(id)
	if err != nil {
		return nil, err
	}
	records[id] = workspaceRecord{SecretHash: ws.SecretHash}
	if err := s.save(records); err != nil {
		return nil, err
	}
	return secret, nil
}

// verifier returns an admission.Verifier seeded with every enrolled
// workspace, for the running relay server.
func (s workspaceStore) verifier() (*admission.Verifier, error) {
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

// list returns the enrolled workspace IDs (sorted by the caller).
func (s workspaceStore) list() ([]string, error) {
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
