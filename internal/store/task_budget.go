package store

import "fmt"

// TreeKnownCost returns the reported cost of the task tree rooted at rootID
// and how many finished runs reported none (ADR-0041 A5). The tree is the
// root plus every descendant by parent_task_id, archived included; the runs
// are purpose='user' runs. known sums run_usage.cost_usd (a running run
// counts once its usage row exists; in-flight cost is never estimated);
// unknownRuns counts finished (not running) runs whose usage row is missing
// or has a NULL cost -- they are never estimated (ADR-0020) and never
// counted in known. Fleet mirrors (fleet_remote_tasks rows whose
// local_task_id is in the tree, ADR-0048 F4) add their reported cost to known
// the same way; a terminal mirror (done, error, stopped) with a NULL cost
// counts as one unknown run. Two queries; fleet off, the second sees no rows.
func (s *Store) TreeKnownCost(rootID int64) (known float64, unknownRuns int, err error) {
	err = s.db.QueryRow(`
		WITH RECURSIVE tree(id) AS (
			SELECT id FROM tasks WHERE id = ?
			UNION
			SELECT t.id FROM tasks t JOIN tree ON t.parent_task_id = tree.id
		)
		SELECT COALESCE(SUM(u.cost_usd), 0),
			COALESCE(SUM(CASE WHEN r.status <> 'running' AND (u.run_id IS NULL OR u.cost_usd IS NULL) THEN 1 ELSE 0 END), 0)
		FROM runs r
		JOIN tree ON tree.id = r.task_id
		LEFT JOIN run_usage u ON u.run_id = r.id
		WHERE r.purpose = 'user'`, rootID,
	).Scan(&known, &unknownRuns)
	if err != nil {
		return 0, 0, fmt.Errorf("tree known cost for task %d: %w", rootID, err)
	}
	var remoteKnown float64
	var remoteUnknown int
	err = s.db.QueryRow(`
		WITH RECURSIVE tree(id) AS (
			SELECT id FROM tasks WHERE id = ?
			UNION
			SELECT t.id FROM tasks t JOIN tree ON t.parent_task_id = tree.id
		)
		SELECT COALESCE(SUM(m.cost_usd), 0),
			COALESCE(SUM(CASE WHEN m.cost_usd IS NULL AND m.state IN ('done', 'error', 'stopped') THEN 1 ELSE 0 END), 0)
		FROM fleet_remote_tasks m
		JOIN tree ON tree.id = m.local_task_id`, rootID,
	).Scan(&remoteKnown, &remoteUnknown)
	if err != nil {
		return 0, 0, fmt.Errorf("tree remote cost for task %d: %w", rootID, err)
	}
	return known + remoteKnown, unknownRuns + remoteUnknown, nil
}
