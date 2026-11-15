package runsock

import (
	"strings"
	"testing"
)

func treeOf(root int, rootStart uint64) ProcTree {
	return FuncProcTree(func(pid int) (ProcInfo, error) {
		switch pid {
		case root:
			return ProcInfo{PPID: 1, Start: rootStart}, nil
		case 600: // a child of the root
			return ProcInfo{PPID: root, Start: 6}, nil
		case 700: // a stranger
			return ProcInfo{PPID: 1, Start: 7}, nil
		}
		return ProcInfo{}, ErrNoProcess
	})
}

// checkCred is the one decision every platform (the Windows pipe included) makes.
func TestCheckCredTable(t *testing.T) {
	const root = 500
	tree := treeOf(root, 50)
	cases := []struct {
		name      string
		pc        PeerCred
		checkUID  bool
		wantUID   int
		tree      ProcTree
		rootStart uint64
		want      string
	}{
		{"descendant, right user", PeerCred{UID: 1000, PID: 600}, true, 1000, tree, 50, ""},
		{"the root itself", PeerCred{UID: 1000, PID: root}, true, 1000, tree, 50, ""},
		{"another user", PeerCred{UID: 1001, PID: 600}, true, 1000, tree, 50, "another user"},
		{"another user ignored where the platform has none (the pipe's DACL decides)", PeerCred{UID: 7, PID: 600}, false, 0, tree, 50, ""},
		{"a stranger", PeerCred{UID: 1000, PID: 700}, true, 1000, tree, 50, "not a process of this run"},
		{"an unknown process", PeerCred{UID: 1000, PID: 800}, true, 1000, tree, 50, "not a process of this run"},
		{"the root was replaced", PeerCred{UID: 1000, PID: 600}, true, 1000, treeOf(root, 51), 50, "not a process of this run"},
		{"pid zero", PeerCred{UID: 1000, PID: 0}, true, 1000, tree, 50, "not a process of this run"},
	}
	for _, c := range cases {
		got := checkCred(c.pc, c.checkUID, c.wantUID, c.tree, root, c.rootStart)
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if strings.ContainsAny(got, "0123456789") {
			t.Errorf("%s: a refusal reason carries no process data: %q", c.name, got)
		}
	}
}

func TestPipeSDDLIsOneSIDOnlyAndProtected(t *testing.T) {
	got, err := pipeSDDL("S-1-5-21-111-222-333-1001")
	if err != nil || got != "D:P(A;;GA;;;S-1-5-21-111-222-333-1001)" {
		t.Fatalf("%q %v", got, err)
	}
	// Exactly one ACE, no inheritance flags, and the DACL is protected.
	if strings.Count(got, "(") != 1 || !strings.HasPrefix(got, "D:P") || strings.Contains(got, ";OI") || strings.Contains(got, ";CI") {
		t.Fatalf("one protected ACE: %q", got)
	}
	for _, bad := range []string{"", "S-1-5-21)(A;;GA;;;WD", "WD", "S-1-5 21", "S-1-5-21-1;"} {
		if _, err := pipeSDDL(bad); err == nil {
			t.Errorf("%q must be refused (no SDDL injection, no well-known aliases)", bad)
		}
	}
}

func TestPipeNameIsPerRunAndLocal(t *testing.T) {
	a, b := pipeName("run-A"), pipeName("run-B")
	if a == b || !strings.HasPrefix(a, `\\.\pipe\smind-browser-`) || strings.Contains(a, "run-A") {
		t.Fatalf("%q %q", a, b)
	}
	if len(sockName("x")) != 12+len(sockSuffix) {
		t.Fatalf("sockName length: %q", sockName("x"))
	}
}
