package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProviderSecretInfo is everything a listing may reveal about a stored
// provider secret: a short fingerprint and the last update time, never the
// value itself (ADR-0046 section 4).
type ProviderSecretInfo struct {
	Fingerprint string
	UpdatedAt   time.Time
}

// SecretFingerprint returns the last 4 hex characters of a secret value's
// sha256 -- enough to tell two keys apart on a card, not enough to recover
// anything about the value.
func SecretFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	hexed := hex.EncodeToString(sum[:])
	return hexed[len(hexed)-4:]
}

// SetProviderSecret stores (or overwrites) one named secret of a provider
// instance. Stored like account credentials: a plain column of the database,
// protected by the file's 0600 mode.
func (s *Store) SetProviderSecret(instanceID, name, value string) error {
	if instanceID == "" || name == "" || value == "" {
		return errors.New("provider secret requires an instance id, a name and a value")
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO provider_secrets (instance_id, name, value, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		instanceID, name, value, now, now); err != nil {
		return fmt.Errorf("set provider secret %q of instance %q: %w", name, instanceID, err)
	}
	return nil
}

// GetProviderSecret returns one secret's value, or ok=false when the
// instance has no secret by that name.
func (s *Store) GetProviderSecret(instanceID, name string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM provider_secrets WHERE instance_id = ? AND name = ?`, instanceID, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get provider secret %q of instance %q: %w", name, instanceID, err)
	}
	return value, true, nil
}

// ClearProviderSecret removes one secret; clearing a secret that is not
// stored is not an error.
func (s *Store) ClearProviderSecret(instanceID, name string) error {
	if _, err := s.db.Exec(`DELETE FROM provider_secrets WHERE instance_id = ? AND name = ?`, instanceID, name); err != nil {
		return fmt.Errorf("clear provider secret %q of instance %q: %w", name, instanceID, err)
	}
	return nil
}

// ListProviderSecrets returns an instance's secrets by name, with only a
// fingerprint of each value -- the values themselves are never returned,
// logged or formatted.
func (s *Store) ListProviderSecrets(instanceID string) (map[string]ProviderSecretInfo, error) {
	rows, err := s.db.Query(`SELECT name, value, updated_at FROM provider_secrets WHERE instance_id = ?`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list provider secrets of instance %q: %w", instanceID, err)
	}
	defer rows.Close()
	out := map[string]ProviderSecretInfo{}
	for rows.Next() {
		var name, value string
		var updatedAt time.Time
		if err := rows.Scan(&name, &value, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan provider secret: %w", err)
		}
		out[name] = ProviderSecretInfo{Fingerprint: SecretFingerprint(value), UpdatedAt: updatedAt.UTC()}
	}
	return out, rows.Err()
}

// DeleteProviderSecrets removes every secret of one instance.
func (s *Store) DeleteProviderSecrets(instanceID string) error {
	if _, err := s.db.Exec(`DELETE FROM provider_secrets WHERE instance_id = ?`, instanceID); err != nil {
		return fmt.Errorf("delete provider secrets of instance %q: %w", instanceID, err)
	}
	return nil
}

// PruneProviderSecrets removes the secrets of every instance not in
// keepInstanceIDs (an empty keep list removes all of them) and returns how
// many rows were deleted. A plugin provider's id ("<plugin>/<provider>")
// always stays: its key survives the plugin being disabled, and the plugin's
// removal deletes it by prefix (ProviderSecretsOfPlugin).
func (s *Store) PruneProviderSecrets(keepInstanceIDs []string) (int, error) {
	var (
		res sql.Result
		err error
	)
	if len(keepInstanceIDs) == 0 {
		res, err = s.db.Exec(`DELETE FROM provider_secrets WHERE instance_id NOT LIKE '%/%'`)
	} else {
		placeholders := strings.Repeat("?,", len(keepInstanceIDs))
		args := make([]any, len(keepInstanceIDs))
		for i, id := range keepInstanceIDs {
			args[i] = id
		}
		res, err = s.db.Exec(
			`DELETE FROM provider_secrets WHERE instance_id NOT LIKE '%/%' AND instance_id NOT IN (`+placeholders[:len(placeholders)-1]+`)`,
			args...,
		)
	}
	if err != nil {
		return 0, fmt.Errorf("prune provider secrets: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune provider secrets: %w", err)
	}
	return int(deleted), nil
}
