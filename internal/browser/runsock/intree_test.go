package runsock

import (
	"errors"
	"testing"
)

func mapTree(m map[int]ProcInfo) FuncProcTree {
	return func(pid int) (ProcInfo, error) {
		info, ok := m[pid]
		if !ok {
			return ProcInfo{}, ErrNoProcess
		}
		return info, nil
	}
}

func TestInTreeTable(t *testing.T) {
	m := map[int]ProcInfo{
		100: {PPID: 1, Start: 1100},
		101: {PPID: 100, Start: 1101},
		102: {PPID: 101, Start: 1102},
		103: {PPID: 102, Start: 1103},
		104: {PPID: 103, Start: 1104},
		// a sibling tree hanging off init, not under 100
		199: {PPID: 1, Start: 1199},
		200: {PPID: 199, Start: 1200},
		// a chain that reaches pid 1 below the root
		2:   {PPID: 1, Start: 1002},
		300: {PPID: 2, Start: 1300},
		// a loop
		400: {PPID: 401, Start: 1400},
		401: {PPID: 400, Start: 1401},
		// the root of the deep chains below
		500: {PPID: 1, Start: 1500},
		// a process whose parent is missing from the tree
		600: {PPID: 601, Start: 1600},
	}
	// a chain of 65 hops from 565 down to root 500
	for p := 501; p <= 565; p++ {
		m[p] = ProcInfo{PPID: p - 1, Start: uint64(1000 + p)}
	}
	// a chain of exactly maxTreeDepth hops from 564 down to root 500

	tests := []struct {
		name       string
		peer, root int
		rootStart  uint64
		want       bool
		wantErr    bool
	}{
		{"root itself", 100, 100, 1100, true, false},
		{"child", 101, 100, 1100, true, false},
		{"grandchild", 102, 100, 1100, true, false},
		{"great-grandchild", 103, 100, 1100, true, false},
		{"sibling tree", 200, 100, 1100, false, false},
		{"chain reaches init", 300, 100, 1100, false, false},
		{"loop", 400, 100, 1100, false, true},
		{"chain too deep (65 hops)", 565, 500, 1500, false, true},
		{"chain at max depth (64 hops)", 564, 500, 1500, true, false},
		{"vanished mid-walk", 600, 100, 1100, false, true},
		{"root start mismatch", 101, 100, 9999, false, false},
		{"peer non-positive", 0, 100, 1100, false, false},
		{"peer negative", -5, 100, 1100, false, false},
		{"root is init", 101, 1, 0, false, false},
		{"root non-positive", 101, 0, 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InTree(mapTree(m), tt.peer, tt.root, tt.rootStart)
			if got != tt.want {
				t.Errorf("InTree(...) = %v, want %v", got, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("InTree(...) error = %v, want error: %v", err, tt.wantErr)
			}
		})
	}

	t.Run("root start changed on second read", func(t *testing.T) {
		rootCalls := 0
		tree := FuncProcTree(func(pid int) (ProcInfo, error) {
			switch pid {
			case 100:
				rootCalls++
				return ProcInfo{PPID: 1, Start: 1100 + uint64(rootCalls)}, nil
			case 101:
				return ProcInfo{PPID: 100, Start: 1101}, nil
			}
			return ProcInfo{}, ErrNoProcess
		})
		// rootStart matches the first read of the root; the re-read sees a
		// different process in the same pid.
		got, err := InTree(tree, 101, 100, 1101)
		if got || err != nil {
			t.Errorf("InTree(...) = %v, %v; want false, nil", got, err)
		}
	})

	t.Run("peer start changed on second read", func(t *testing.T) {
		peerCalls := 0
		tree := FuncProcTree(func(pid int) (ProcInfo, error) {
			switch pid {
			case 101:
				peerCalls++
				return ProcInfo{PPID: 100, Start: 1101 + uint64(peerCalls)}, nil
			case 100:
				return ProcInfo{PPID: 1, Start: 1100}, nil
			}
			return ProcInfo{}, ErrNoProcess
		})
		got, err := InTree(tree, 101, 100, 1100)
		if got || err != nil {
			t.Errorf("InTree(...) = %v, %v; want false, nil", got, err)
		}
	})
}

func TestStartOf(t *testing.T) {
	m := map[int]ProcInfo{42: {PPID: 1, Start: 1042}}
	start, err := StartOf(mapTree(m), 42)
	if err != nil {
		t.Fatalf("StartOf(...) error = %v, want nil", err)
	}
	if start != 1042 {
		t.Errorf("StartOf(...) = %d, want 1042", start)
	}
	if _, err := StartOf(mapTree(m), 43); !errors.Is(err, ErrNoProcess) {
		t.Errorf("StartOf(missing) error = %v, want ErrNoProcess", err)
	}
}
