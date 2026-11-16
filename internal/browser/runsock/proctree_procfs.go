package runsock

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// parseProcStat parses a /proc/<pid>/stat line into ProcInfo. comm can
// contain spaces and ')' characters, so the payload is everything after the
// last ')'. fields[0] is state, fields[1] is ppid and fields[19] is starttime
// (field 22 of the file). Used on Linux; testable everywhere.
func parseProcStat(b []byte) (ProcInfo, error) {
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return ProcInfo{}, fmt.Errorf("runsock: malformed stat line %q: no ')'", b)
	}
	fields := strings.Fields(string(b[i+1:]))
	if len(fields) < 20 {
		return ProcInfo{}, fmt.Errorf("runsock: malformed stat line %q: got %d fields after comm, want at least 20", b, len(fields))
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return ProcInfo{}, fmt.Errorf("runsock: malformed stat line %q: bad ppid: %w", b, err)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return ProcInfo{}, fmt.Errorf("runsock: malformed stat line %q: bad starttime: %w", b, err)
	}
	return ProcInfo{PPID: ppid, Start: start}, nil
}
