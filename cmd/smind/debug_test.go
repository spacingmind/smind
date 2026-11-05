package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func stubDebug(t *testing.T, result string) {
	t.Helper()
	prevDial, prevProc := debugDial, processMemory
	t.Cleanup(func() { debugDial, processMemory = prevDial, prevProc })
	debugDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, _ any, out any) error {
			if method != "debug.memory" {
				t.Errorf("method = %q", method)
			}
			return json.Unmarshal([]byte(result), out)
		}, func() {}, nil
	}
	processMemory = func(int) (int64, int64) { return 150 << 20, 140 << 20 }
}

const debugMemoryFixture = `{"pid":7,"uptimeSeconds":3600,"goVersion":"go1.x","numGoroutine":19,
"mem":{"heapAlloc":52428800,"heapInuse":60000000,"heapIdle":1,"heapReleased":0,"heapSys":70000000,"stackInuse":1,"sys":90000000,"numGC":4,"pauseTotalNs":1000,"nextGC":100000000},
"runs":{"runsInMemory":200,"runningRuns":1,"residentRuns":3,"residentBytes":33554432,"budgetBytes":67108864,"runningHistoryBytes":1048576,"loads":5,"evictions":2,"storeReads":9}}`

func TestDebugMemoryCommandPrintsHeapAndRegistry(t *testing.T) {
	stubDebug(t, debugMemoryFixture)
	var out, errb bytes.Buffer
	if code := runDebug(context.Background(), []string{"memory"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"pid 7", "phys_footprint   140.0 MiB", "rss              150.0 MiB", "heap alloc       50.0 MiB", "200 (1 running)", "3 runs, 32.0 MiB of 64.0 MiB budget", "evictions 2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if code := runDebug(context.Background(), []string{"memory", "--json"}, &out, &errb); code != 0 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"physFootprintBytes": 146800640`) {
		t.Fatalf("json output (%d): %s", code, out.String())
	}
}

func TestDebugMemoryCommandUsage(t *testing.T) {
	var out, errb bytes.Buffer
	for _, args := range [][]string{nil, {"heap"}, {"memory", "--x"}} {
		if code := runDebug(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestParseFootprintBytes(t *testing.T) {
	out := "====\nzsh [29692]: 64-bit    Footprint: 2572720 B (16384 bytes per page)\n"
	if got := parseFootprintBytes(out); got != 2572720 {
		t.Fatalf("got %d", got)
	}
	if parseFootprintBytes("no such line") != 0 || parseFootprintBytes("Footprint: 12 MB") != 0 {
		t.Fatal("unparseable input must give 0")
	}
}
