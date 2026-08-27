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
