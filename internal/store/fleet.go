package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Link directions on fleet_links (ADR-0048 F1a): outbound is a link we
// dial, inbound one the remote dials.
const (
	FleetOutbound = "outbound"
	FleetInbound  = "inbound"
)

// Fleet sentinel errors. Compare with errors.Is; the queries wrap them
// with the record they name.
var (
	ErrFleetNotFound   = errors.New("store: fleet record not found")
	ErrFleetAliasTaken = errors.New("store: fleet alias already used")
	ErrFleetHostExists = errors.New("store: fleet host already exists")
)

// FleetHost is one remote smind in the fleet: its stable host id (derived
// from its public key), the alias the user addresses it by, the public key
// itself, and its current lifecycle state.
type FleetHost struct {
	HostID    string
	Alias     string
	Pub       []byte
	Label     string
	State     string
	CreatedAt time.Time
}

// FleetLink is one direction of the pairing with a host: whether it is
// enabled, and the grant (what the far side may do) and transport config,
// both opaque JSON to the store.
type FleetLink struct {
	HostID        string
	Direction     string
	Enabled       bool
	GrantJSON     string
	TransportJSON string
	UpdatedAt     time.Time
}

// fleetDirectionOK reports whether d is one of the two link directions.
func fleetDirectionOK(d string) bool {
	return d == FleetOutbound || d == FleetInbound
}

// CreateFleetPair inserts a host and all of its links in one transaction:
// either the whole pair lands or nothing does. Duplicate host ids and
// aliases are detected by looking for the row before inserting (inside the
// transaction, so a concurrent writer cannot slip between check and
// insert) rather than by parsing the driver's constraint error.
func (s *Store) CreateFleetPair(h FleetHost, links []FleetLink) error {
	for _, l := range links {
		if !fleetDirectionOK(l.Direction) {
			return fmt.Errorf("create fleet pair %s: link direction %q", h.HostID, l.Direction)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("create fleet pair %s: begin: %w", h.HostID, err)
	}
	defer tx.Rollback()

	var one int
	err = tx.QueryRow(`SELECT 1 FROM fleet_hosts WHERE host_id = ?`, h.HostID).Scan(&one)
	switch {
	case err == nil:
		return fmt.Errorf("create fleet pair %s: %w", h.HostID, ErrFleetHostExists)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("create fleet pair %s: check host: %w", h.HostID, err)
	}
	err = tx.QueryRow(`SELECT 1 FROM fleet_hosts WHERE alias = ?`, h.Alias).Scan(&one)
	switch {
	case err == nil:
		return fmt.Errorf("create fleet pair %s: %w", h.HostID, ErrFleetAliasTaken)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("create fleet pair %s: check alias: %w", h.HostID, err)
	}

	now := time.Now().UTC()
	if h.CreatedAt.IsZero() {
		h.CreatedAt = now
	}
	if _, err := tx.Exec(
		`INSERT INTO fleet_hosts (host_id, alias, pub, label, state, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		h.HostID, h.Alias, h.Pub, h.Label, h.State, h.CreatedAt,
	); err != nil {
		return fmt.Errorf("create fleet pair %s: insert host: %w", h.HostID, err)
	}
	for _, l := range links {
		if l.UpdatedAt.IsZero() {
			l.UpdatedAt = now
		}
		if _, err := tx.Exec(
			`INSERT INTO fleet_links (host_id, direction, enabled, grant_json, transport_json, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			l.HostID, l.Direction, l.Enabled, l.GrantJSON, l.TransportJSON, l.UpdatedAt,
		); err != nil {
			return fmt.Errorf("create fleet pair %s: insert %s link: %w", h.HostID, l.Direction, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create fleet pair %s: commit: %w", h.HostID, err)
	}
	return nil
}

const fleetHostColumns = `host_id, alias, pub, label, state, created_at`

func scanFleetHost(sc rowScanner) (FleetHost, error) {
	var h FleetHost
	if err := sc.Scan(&h.HostID, &h.Alias, &h.Pub, &h.Label, &h.State, &h.CreatedAt); err != nil {
		return FleetHost{}, err
	}
	h.CreatedAt = h.CreatedAt.UTC()
	return h, nil
}

// GetFleetHost returns the host with the given host id.
func (s *Store) GetFleetHost(hostID string) (FleetHost, error) {
	h, err := scanFleetHost(s.db.QueryRow(`SELECT `+fleetHostColumns+` FROM fleet_hosts WHERE host_id = ?`, hostID))
	if errors.Is(err, sql.ErrNoRows) {
		return FleetHost{}, fmt.Errorf("get fleet host %s: %w", hostID, ErrFleetNotFound)
	}
	if err != nil {
		return FleetHost{}, fmt.Errorf("get fleet host %s: %w", hostID, err)
	}
	return h, nil
}

// GetFleetHostByAlias returns the host the user addresses by that alias.
func (s *Store) GetFleetHostByAlias(alias string) (FleetHost, error) {
	h, err := scanFleetHost(s.db.QueryRow(`SELECT `+fleetHostColumns+` FROM fleet_hosts WHERE alias = ?`, alias))
	if errors.Is(err, sql.ErrNoRows) {
		return FleetHost{}, fmt.Errorf("get fleet host by alias %s: %w", alias, ErrFleetNotFound)
	}
	if err != nil {
		return FleetHost{}, fmt.Errorf("get fleet host by alias %s: %w", alias, err)
	}
	return h, nil
}

// ListFleetHosts returns every host ordered by alias.
func (s *Store) ListFleetHosts() ([]FleetHost, error) {
	rows, err := s.db.Query(`SELECT ` + fleetHostColumns + ` FROM fleet_hosts ORDER BY alias`)
	if err != nil {
		return nil, fmt.Errorf("list fleet hosts: %w", err)
	}
	defer rows.Close()
	var out []FleetHost
	for rows.Next() {
		h, err := scanFleetHost(rows)
		if err != nil {
			return nil, fmt.Errorf("list fleet hosts: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CountFleetHosts returns how many hosts are stored.
func (s *Store) CountFleetHosts() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM fleet_hosts`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count fleet hosts: %w", err)
	}
	return n, nil
}

// UpdateFleetHost sets a host's alias, label and state. An alias another
// host already carries is rejected; the host must exist.
func (s *Store) UpdateFleetHost(hostID, alias, label, state string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("update fleet host %s: begin: %w", hostID, err)
	}
	defer tx.Rollback()

	var one int
	err = tx.QueryRow(`SELECT 1 FROM fleet_hosts WHERE host_id = ?`, hostID).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("update fleet host %s: %w", hostID, ErrFleetNotFound)
	case err != nil:
		return fmt.Errorf("update fleet host %s: check host: %w", hostID, err)
	}
	err = tx.QueryRow(`SELECT 1 FROM fleet_hosts WHERE alias = ? AND host_id != ?`, alias, hostID).Scan(&one)
	switch {
	case err == nil:
		return fmt.Errorf("update fleet host %s: %w", hostID, ErrFleetAliasTaken)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("update fleet host %s: check alias: %w", hostID, err)
	}

	if _, err := tx.Exec(
		`UPDATE fleet_hosts SET alias = ?, label = ?, state = ? WHERE host_id = ?`,
		alias, label, state, hostID,
	); err != nil {
		return fmt.Errorf("update fleet host %s: %w", hostID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update fleet host %s: commit: %w", hostID, err)
	}
	return nil
}

// DeleteFleetHost removes a host and its links in one transaction. The
// links are deleted explicitly rather than left to the schema's ON DELETE
// CASCADE, so a database opened with foreign keys off (or a driver that
// ignores it) cannot leave orphaned rows behind.
func (s *Store) DeleteFleetHost(hostID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete fleet host %s: begin: %w", hostID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM fleet_links WHERE host_id = ?`, hostID); err != nil {
		return fmt.Errorf("delete fleet host %s: links: %w", hostID, err)
	}
	res, err := tx.Exec(`DELETE FROM fleet_hosts WHERE host_id = ?`, hostID)
	if err != nil {
		return fmt.Errorf("delete fleet host %s: host: %w", hostID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete fleet host %s: %w", hostID, err)
	}
	if n == 0 {
		return fmt.Errorf("delete fleet host %s: %w", hostID, ErrFleetNotFound)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete fleet host %s: commit: %w", hostID, err)
	}
	return nil
}

// PutFleetLink upserts one direction's link for a host, stamping
// updated_at now. The host must exist and the direction must be one of
// the two.
func (s *Store) PutFleetLink(l FleetLink) error {
	if !fleetDirectionOK(l.Direction) {
		return fmt.Errorf("put fleet link %s: direction %q", l.HostID, l.Direction)
	}
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM fleet_hosts WHERE host_id = ?`, l.HostID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("put fleet link %s: %w", l.HostID, ErrFleetNotFound)
	}
	if err != nil {
		return fmt.Errorf("put fleet link %s: check host: %w", l.HostID, err)
	}

	l.UpdatedAt = time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO fleet_links (host_id, direction, enabled, grant_json, transport_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id, direction) DO UPDATE SET
			enabled = excluded.enabled,
			grant_json = excluded.grant_json,
			transport_json = excluded.transport_json,
			updated_at = excluded.updated_at`,
		l.HostID, l.Direction, l.Enabled, l.GrantJSON, l.TransportJSON, l.UpdatedAt); err != nil {
		return fmt.Errorf("put fleet link %s %s: %w", l.HostID, l.Direction, err)
	}
	return nil
}

const fleetLinkColumns = `host_id, direction, enabled, grant_json, transport_json, updated_at`

func scanFleetLink(sc rowScanner) (FleetLink, error) {
	var l FleetLink
	if err := sc.Scan(&l.HostID, &l.Direction, &l.Enabled, &l.GrantJSON, &l.TransportJSON, &l.UpdatedAt); err != nil {
		return FleetLink{}, err
	}
	l.UpdatedAt = l.UpdatedAt.UTC()
	return l, nil
}

// GetFleetLink returns one direction's link for a host.
func (s *Store) GetFleetLink(hostID, direction string) (FleetLink, error) {
	l, err := scanFleetLink(s.db.QueryRow(
		`SELECT `+fleetLinkColumns+` FROM fleet_links WHERE host_id = ? AND direction = ?`, hostID, direction,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return FleetLink{}, fmt.Errorf("get fleet link %s %s: %w", hostID, direction, ErrFleetNotFound)
	}
	if err != nil {
		return FleetLink{}, fmt.Errorf("get fleet link %s %s: %w", hostID, direction, err)
	}
	return l, nil
}

// ListFleetLinks returns a host's links ordered by direction.
func (s *Store) ListFleetLinks(hostID string) ([]FleetLink, error) {
	rows, err := s.db.Query(`SELECT `+fleetLinkColumns+` FROM fleet_links WHERE host_id = ? ORDER BY direction`, hostID)
	if err != nil {
		return nil, fmt.Errorf("list fleet links %s: %w", hostID, err)
	}
	defer rows.Close()
	var out []FleetLink
	for rows.Next() {
		l, err := scanFleetLink(rows)
		if err != nil {
			return nil, fmt.Errorf("list fleet links %s: %w", hostID, err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
