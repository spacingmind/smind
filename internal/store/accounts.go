package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrIdentityInUse reports an account identity already owned by a different
// row of the same provider (the partial unique index on
// accounts(provider, identity)).
var ErrIdentityInUse = errors.New("account identity already in use")

const accountColumns = `id, provider, label, credential_type, credential_data, COALESCE(models, ''), COALESCE(identity, ''), created_at, updated_at`

func scanAccount(sc interface{ Scan(...any) error }) (Account, error) {
	var a Account
	err := sc.Scan(&a.ID, &a.Provider, &a.Label, &a.CredentialType, &a.CredentialData, &a.Models, &a.Identity, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

// CreateAccount inserts a new account, stamping created_at/updated_at. A
// non-empty a.Identity is stored (a collision with another row of the same
// provider returns ErrIdentityInUse); "" stores NULL.
func (s *Store) CreateAccount(a Account) (Account, error) {
	now := time.Now().UTC()
	a.CreatedAt, a.UpdatedAt = now, now

	res, err := s.db.Exec(
		`INSERT INTO accounts (provider, label, credential_type, credential_data, models, identity, created_at, updated_at)
		 VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		a.Provider, a.Label, a.CredentialType, a.CredentialData, a.Models, a.Identity, a.CreatedAt, a.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return Account{}, fmt.Errorf("insert account: %w", ErrIdentityInUse)
	}
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

// SetAccountModels replaces the account's models JSON ("" clears it back to
// NULL, i.e. no list), stamping a new updated_at. A nonexistent id is a
// clear not-found error.
func (s *Store) SetAccountModels(id int64, modelsJSON string) (Account, error) {
	if _, err := s.GetAccount(id); err != nil {
		return Account{}, fmt.Errorf("set account %d models: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE accounts SET models = NULLIF(?, ''), updated_at = ? WHERE id = ?`,
		modelsJSON, time.Now().UTC(), id,
	); err != nil {
		return Account{}, fmt.Errorf("set account %d models: %w", id, err)
	}
	return s.GetAccount(id)
}

// ReplaceAccountCredential swaps both credential_data and credential_type of
// the account with the given id (a swap may change the type -- an api_key
// account replaced by an OAuth credential, or vice versa), stamping a new
// updated_at. Unlike UpdateAccountCredential, which only replaces
// credential_data on the refresh path, this is the account.updateCredential
// RPC's store call. A credential change also clears the account's passive
// quota cooldowns (ADR-0028 §7); the refresh path does not. A nonexistent id
// is a clear not-found error. The account's identity (ADR-0033 D6) described
// the old credential, so it is cleared in the same UPDATE; the OAuth re-login
// path uses the Keeping/Adopting variants instead.
func (s *Store) ReplaceAccountCredential(id int64, credentialType, credentialData string) (Account, error) {
	return s.replaceAccountCredential(id, credentialType, credentialData, "", false)
}

// ReplaceAccountCredentialKeepingIdentity is ReplaceAccountCredential for the
// OAuth re-login of the same vendor account (ADR-0033 D6): the credential is
// swapped and the stored identity is left as is.
func (s *Store) ReplaceAccountCredentialKeepingIdentity(id int64, credentialType, credentialData string) (Account, error) {
	return s.replaceAccountCredential(id, credentialType, credentialData, "", true)
}

// ReplaceAccountCredentialAdoptingIdentity is ReplaceAccountCredential plus
// setting the account's identity in the same transaction (ADR-0033 D6:
// re-login in place, adopting the identity for a legacy row). identity must
// be non-empty; another row of the same provider owning it returns
// ErrIdentityInUse and changes nothing.
func (s *Store) ReplaceAccountCredentialAdoptingIdentity(id int64, credentialType, credentialData, identity string) (Account, error) {
	if identity == "" {
		return Account{}, fmt.Errorf("update account %d credential: empty identity", id)
	}
	return s.replaceAccountCredential(id, credentialType, credentialData, identity, false)
}

// GetAccountByIdentity returns the account of the given provider owning
// identity. A missing row (and an empty identity, which never matches) is
// sql.ErrNoRows, checkable with errors.Is.
func (s *Store) GetAccountByIdentity(provider, identity string) (Account, error) {
	if identity == "" {
		return Account{}, fmt.Errorf("get account by identity: %w", sql.ErrNoRows)
	}
	a, err := scanAccount(s.db.QueryRow(
		`SELECT `+accountColumns+` FROM accounts WHERE provider = ? AND identity = ?`, provider, identity))
	if err != nil {
		return Account{}, fmt.Errorf("get account by identity: %w", err)
	}
	return a, nil
}

// SetAccountIdentity stores identity on the account with the given id
// without touching its credential ("" clears it back to NULL). Another row
// of the same provider owning it returns ErrIdentityInUse. A nonexistent id
// is a not-found error.
func (s *Store) SetAccountIdentity(id int64, identity string) (Account, error) {
	if _, err := s.GetAccount(id); err != nil {
		return Account{}, fmt.Errorf("set account %d identity: %w", id, err)
	}
	if _, err := s.db.Exec(
		`UPDATE accounts SET identity = NULLIF(?, ''), updated_at = ? WHERE id = ?`,
		identity, time.Now().UTC(), id,
	); err != nil {
		if isUniqueViolation(err) {
			return Account{}, fmt.Errorf("set account %d identity: %w", id, ErrIdentityInUse)
		}
		return Account{}, fmt.Errorf("set account %d identity: %w", id, err)
	}
	return s.GetAccount(id)
}

// replaceAccountCredential does the work of the replace methods. keepIdentity
// leaves the stored identity untouched; otherwise it is set to identity ("" =
// cleared to NULL) in the same UPDATE as the credential.
func (s *Store) replaceAccountCredential(id int64, credentialType, credentialData, identity string, keepIdentity bool) (Account, error) {
	if _, err := s.GetAccount(id); err != nil {
		return Account{}, fmt.Errorf("update account %d credential: %w", id, err)
	}
	// One transaction: a failed cooldown delete must not leave the new credential
	// in place with the old credential's cooldowns still alive.
	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, fmt.Errorf("update account %d credential: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE accounts SET credential_type = ?, credential_data = ?, identity = CASE WHEN ? THEN identity ELSE NULLIF(?, '') END, updated_at = ? WHERE id = ?`,
		credentialType, credentialData, keepIdentity, identity, time.Now().UTC(), id,
	); err != nil {
		if isUniqueViolation(err) {
			return Account{}, fmt.Errorf("update account %d credential: %w", id, ErrIdentityInUse)
		}
		return Account{}, fmt.Errorf("update account %d credential: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM quota_cooldown WHERE account_id = ?`, id); err != nil {
		return Account{}, fmt.Errorf("update account %d credential: delete quota cooldowns: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
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
	if _, err := s.db.Exec(`DELETE FROM quota_poll_state WHERE account_id = ?`, id); err != nil {
		return fmt.Errorf("delete account %d: delete quota poll state: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM quota_cooldown WHERE account_id = ?`, id); err != nil {
		return fmt.Errorf("delete account %d: delete quota cooldowns: %w", id, err)
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
	a, err := scanAccount(s.db.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id))
	if err != nil {
		return Account{}, fmt.Errorf("get account %d: %w", id, err)
	}
	return a, nil
}

// ListAccounts returns all accounts, ordered by id.
func (s *Store) ListAccounts() ([]Account, error) {
	rows, err := s.db.Query(
		`SELECT ` + accountColumns + ` FROM accounts ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
