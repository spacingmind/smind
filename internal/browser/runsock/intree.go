package runsock

import "errors"

// maxTreeDepth bounds the ancestry walk so a corrupt or adversarial process
// tree cannot make InTree walk forever.
const maxTreeDepth = 64

// InTree reports whether peer is root itself or a descendant of root, failing
// closed: any doubt (missing process, pid reuse, a loop in the tree, a tree
// too deep) yields false rather than true.
//
// rootStart must be the Start of root captured when the root process was
// launched (StartOf). Comparing it at the root and re-reading both ends
// narrows the race where a pid is reused by another process between the
// launch and the check.
func InTree(tree ProcTree, peer, root int, rootStart uint64) (bool, error) {
	if peer <= 0 || root <= 1 {
		// root 1 is init, which everything descends from; a non-positive pid
		// is not a process. Neither can gate anything.
		return false, nil
	}
	first, err := tree.Info(peer)
	if err != nil {
		return false, err
	}
	cur, info := peer, first
	seen := make(map[int]bool)
	for i := 0; i <= maxTreeDepth; i++ {
		if cur == root {
			// The root pid must still name the process the caller recorded;
			// otherwise the pid was reused by an unrelated process.
			if info.Start != rootStart {
				return false, nil
			}
			// Re-read both ends: if either side changed identity mid-walk,
			// a pid was reused and the ancestry just observed is stale.
			again, err := tree.Info(root)
			if err != nil {
				return false, err
			}
			if again.Start != rootStart {
				return false, nil
			}
			again2, err := tree.Info(peer)
			if err != nil {
				return false, err
			}
			if again2.Start != first.Start {
				return false, nil
			}
			return true, nil
		}
		if seen[cur] {
			// A cycle in the parent chain: the data is inconsistent, so the
			// peer's ancestry cannot be established.
			return false, errors.New("runsock: loop in the process tree")
		}
		seen[cur] = true
		if info.PPID <= 1 || info.PPID == cur {
			// The chain reached init (or was reparented to it) without
			// passing through root, or claims to be its own parent: the
			// peer is not under root.
			return false, nil
		}
		child := info
		cur = info.PPID
		info, err = tree.Info(cur)
		if err != nil {
			// A process vanished mid-walk; its recorded ancestry is now
			// unverifiable.
			return false, err
		}
		if info.Start > child.Start {
			// A parent cannot have started after its child: the pid was reused
			// by a younger process after the real parent exited (Windows never
			// reparents), so this chain does not lead to the process that
			// actually created the child.
			return false, nil
		}
	}
	return false, errors.New("runsock: process tree too deep")
}

// StartOf returns the opaque start time of pid, to capture when a process is
// launched and later hand to InTree as rootStart.
func StartOf(tree ProcTree, pid int) (uint64, error) {
	info, err := tree.Info(pid)
	if err != nil {
		return 0, err
	}
	return info.Start, nil
}
