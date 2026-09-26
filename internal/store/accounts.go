package store

import (
	"fmt"
	"time"
)

// CreateAccount inserts a new account, stamping created_at/updated_at.
func (s *Store) CreateAccount(a Account) (Account, error) {
	now := time.Now().UTC()
	a.CreatedAt, a.UpdatedAt = now, now

	res, err := s.db.Exec(
		`INSERT INTO accounts (provider, label, credential_type, credential_data, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.Provider, a.Label, a.CredentialType, a.CredentialData, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return Account{}, fmt.Errorf("insert account: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, fmt.Errorf("account id: %w", err)
	}
	a.ID = id
	return a, nil
}

// UpdateAccountCredential replaces an account's credential_data, stamping a
// new updated_at, and returns the updated account. Used by EnsureFresh's
// refresh path, where the credential type cannot change.
func (s *Store) UpdateAccountCredential(id int64, credentialData string) (Account, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE accounts SET credential_data = ?, updated_at = ? WHERE id = ?`,
		credentialData, now, id,
	)
	if err != nil {
		return Account{}, fmt.Errorf("update account %d credential: %w", id, err)
	}
	return s.GetAccount(id)
}

// RenameAccount relabels the account with the given id, stamping a new
// updated_at. Renaming a nonexistent id is a clear not-found error (via
// GetAccount), never a silent no-op -- matching UpdateAgentProfile's
// convention.
func (s *Store) RenameAccount(id int64, label string) (Account, error) {
	if _, err := s.GetAccount(id); err != nil {
		return Account{}, fmt.Errorf("rename account %d: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE accounts SET label = ?, updated_at = ? WHERE id = ?`,
		label, time.Now().UTC(), id,
	); err != nil {
		return Account{}, fmt.Errorf("rename account %d: %w", id, err)
	}
	return s.GetAccount(id)
}

// ReplaceAccountCredential swaps both credential_data and credential_type of
// the account with the given id (a swap may change the type -- an api_key
// account replaced by an OAuth credential, or vice versa), stamping a new
// updated_at. Unlike UpdateAccountCredential, which only replaces
// credential_data on the refresh path, this is the account.updateCredential
// RPC's store call. A nonexistent id is a clear not-found error.
func (s *Store) ReplaceAccountCredential(id int64, credentialType, credentialData string) (Account, error) {
	if _, err := s.GetAccount(id); err != nil {
		return Account{}, fmt.Errorf("update account %d credential: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE accounts SET credential_type = ?, credential_data = ?, updated_at = ? WHERE id = ?`,
		credentialType, credentialData, time.Now().UTC(), id,
	); err != nil {
		return Account{}, fmt.Errorf("update account %d credential: %w", id, err)
	}
	return s.GetAccount(id)
}

// DeleteAccount permanently removes the account with the given id and every
// row referencing it, in FK-safe child-before-parent order: routing
// decisions (including the session-affinity rows -- no session stays pinned
// to the deleted account), quota snapshots, workspace account links, then
// the accounts row itself (ADR-0015). Deleting a nonexistent id is a clear
// not-found error (via GetAccount), never a silent no-op.
func (s *Store) DeleteAccount(id int64) error {
	if _, err := s.GetAccount(id); err != nil {
		return fmt.Errorf("delete account %d: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM routing_decisions WHERE account_id = ?`, id); err != nil {
		return fmt.Errorf("delete account %d: delete routing decisions: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM quota_snapshots WHERE account_id = ?`, id); err != nil {
		return fmt.Errorf("delete account %d: delete quota snapshots: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM workspace_accounts WHERE account_id = ?`, id); err != nil {
		return fmt.Errorf("delete account %d: delete workspace accounts: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM accounts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete account %d: %w", id, err)
	}
	return nil
}

// GetAccount returns the account with the given id.
func (s *Store) GetAccount(id int64) (Account, error) {
	var a Account
	err := s.db.QueryRow(
		`SELECT id, provider, label, credential_type, credential_data, created_at, updated_at
		 FROM accounts WHERE id = ?`, id,
	).Scan(&a.ID, &a.Provider, &a.Label, &a.CredentialType, &a.CredentialData, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return Account{}, fmt.Errorf("get account %d: %w", id, err)
	}
	return a, nil
}

// ListAccounts returns all accounts, ordered by id.
func (s *Store) ListAccounts() ([]Account, error) {
	rows, err := s.db.Query(
		`SELECT id, provider, label, credential_type, credential_data, created_at, updated_at
		 FROM accounts ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Provider, &a.Label, &a.CredentialType, &a.CredentialData, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
