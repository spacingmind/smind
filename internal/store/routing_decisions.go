package store

import "fmt"

// CreateRoutingDecision inserts a new routing decision.
func (s *Store) CreateRoutingDecision(d RoutingDecision) (RoutingDecision, error) {
	res, err := s.db.Exec(
		`INSERT INTO routing_decisions (session_key, account_id, policy, decided_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		d.SessionKey, d.AccountID, d.Policy, d.DecidedAt, d.ExpiresAt,
	)
	if err != nil {
		return RoutingDecision{}, fmt.Errorf("insert routing decision: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RoutingDecision{}, fmt.Errorf("routing decision id: %w", err)
	}
	d.ID = id
	return d, nil
}

// GetRoutingDecision returns the routing decision with the given id.
func (s *Store) GetRoutingDecision(id int64) (RoutingDecision, error) {
	var d RoutingDecision
	err := s.db.QueryRow(
		`SELECT id, session_key, account_id, policy, decided_at, expires_at
		 FROM routing_decisions WHERE id = ?`, id,
	).Scan(&d.ID, &d.SessionKey, &d.AccountID, &d.Policy, &d.DecidedAt, &d.ExpiresAt)
	if err != nil {
		return RoutingDecision{}, fmt.Errorf("get routing decision %d: %w", id, err)
	}
	return d, nil
}

// ListRoutingDecisions returns all routing decisions, ordered by id.
func (s *Store) ListRoutingDecisions() ([]RoutingDecision, error) {
	rows, err := s.db.Query(
		`SELECT id, session_key, account_id, policy, decided_at, expires_at
		 FROM routing_decisions ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list routing decisions: %w", err)
	}
	defer rows.Close()

	var decisions []RoutingDecision
	for rows.Next() {
		var d RoutingDecision
		if err := rows.Scan(&d.ID, &d.SessionKey, &d.AccountID, &d.Policy, &d.DecidedAt, &d.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan routing decision: %w", err)
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}
