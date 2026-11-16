package runsock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

const sockSuffix = ".sock"

// maxConnsPerRun bounds the live connections of one run's socket.
const maxConnsPerRun = 8

// Conn is what the child gets from Dial: a duplex byte stream with deadlines (a
// net.Conn on unix sockets, a pipe handle on Windows).
type Conn interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}

// sockName is the stable short name of a run's socket or pipe: 12 hex characters
// of the run id's hash plus a suffix.
func sockName(runID string) string {
	sum := sha256.Sum256([]byte(runID))
	return hex.EncodeToString(sum[:6]) + sockSuffix
}

// checkCred says why a connection must be refused ("" means allowed). It is the
// one decision every platform makes, over interfaces so it is testable without a
// socket: the peer is the daemon's user (where the platform has user ids) AND a
// process of the run's tree (the run's root process itself, unchanged since the
// socket was created, or a descendant of it). Reasons are FIXED strings: they go
// into a bounded refusal record and must carry no pid or path.
func checkCred(pc PeerCred, checkUID bool, wantUID int, tree ProcTree, root int, rootStart uint64) string {
	if checkUID && int64(pc.UID) != int64(wantUID) {
		return "another user"
	}
	ok, err := InTree(tree, pc.PID, root, rootStart)
	if err != nil || !ok {
		return "not a process of this run"
	}
	return ""
}

// pipeSDDL is the security descriptor of a Windows run pipe: the DACL is
// protected (no inherited entries) and grants generic-all to one user SID only,
// so no other user, service or remote client can even open the pipe.
func pipeSDDL(sid string) (string, error) {
	if sid == "" {
		return "", fmt.Errorf("runsock: no user SID")
	}
	for _, c := range sid {
		if !(c == 'S' || c == '-' || (c >= '0' && c <= '9')) {
			return "", fmt.Errorf("runsock: %q is not a SID", sid)
		}
	}
	return "D:P(A;;GA;;;" + sid + ")", nil
}

// pipeName is the name of a run's pipe (Windows).
func pipeName(runID string) string { return `\\.\pipe\smind-browser-` + sockName(runID)[:12] }
