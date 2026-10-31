package permpolicy

import (
	"bytes"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPlatform_WindowsKeepsEngineOff(t *testing.T) {
	old := HostGOOS
	HostGOOS = "windows"
	t.Cleanup(func() { HostGOOS = old })

	e, s, _ := newTestEngine(t, true, goTestPolicy())
	d, ok := e.Evaluate(dReq(t, "git stash"))
	if ok {
		t.Fatal("Evaluate on Windows must report ok=false")
	}
	if !reflect.DeepEqual(d, Decision{}) {
		t.Fatalf("Evaluate = %+v; want the zero Decision", d)
	}
	if n := len(auditRows(t, s)); n != 0 {
		t.Fatalf("audit rows = %d, want 0", n)
	}
	if e.Status().Enabled {
		t.Fatal("Status().Enabled = true on Windows; want false")
	}
}

func TestPlatform_WindowsWarnsOnce(t *testing.T) {
	old := HostGOOS
	HostGOOS = "windows"
	t.Cleanup(func() { HostGOOS = old })

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	e, _, _ := newTestEngine(t, true, goTestPolicy())
	req := dReq(t, "git stash")
	e.Evaluate(req)
	e.Evaluate(req)
	e.Status()
	if n := strings.Count(buf.String(), "not supported on Windows"); n != 1 {
		t.Fatalf("unsupported warnings = %d, want exactly 1; log = %q", n, buf.String())
	}
}

func TestPlatform_NoWarningWhenConfigOff(t *testing.T) {
	old := HostGOOS
	HostGOOS = "windows"
	t.Cleanup(func() { HostGOOS = old })

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	e, _, _ := newTestEngine(t, false, goTestPolicy())
	e.Evaluate(dReq(t, "git stash"))
	if buf.Len() != 0 {
		t.Fatalf("log = %q; want empty when the config switch is off", buf.String())
	}
}

func TestPlatform_PosixEngineStillOn(t *testing.T) {
	old := HostGOOS
	HostGOOS = "linux"
	t.Cleanup(func() { HostGOOS = old })

	e, _, _ := newTestEngine(t, true, goTestPolicy())
	if _, ok := e.Evaluate(dReq(t, "go test ./...")); !ok {
		t.Fatal("Evaluate on a POSIX host must report ok=true")
	}
}
