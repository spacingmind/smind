package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/crash"
)

// serve-wires-crash-recorder: initCrash points the default recorder at
// <smind home>/crashes without touching the disk, and cmdServe calls it
// before it listens or starts a goroutine.
func TestServeWiresCrashRecorder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	t.Cleanup(func() { crash.Init(crash.Config{}) })
	initCrash()
	if crash.Default() == nil {
		t.Fatal("initCrash did not set the default recorder")
	}
	if _, err := os.Stat(filepath.Join(home, "crashes")); !os.IsNotExist(err) {
		t.Fatalf("initCrash must not create the directory (stat err = %v)", err)
	}
	path := crash.Default().Record("test.site", "boom", []byte("x.Fn()\n"))
	if want := filepath.Join(home, "crashes"); filepath.Dir(path) != want {
		t.Fatalf("report written to %q, want it under %q", path, want)
	}

	src, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src[strings.Index(string(src), "func cmdServe("):])
	initAt := strings.Index(body, "initCrash()")
	if initAt < 0 {
		t.Fatal("cmdServe never calls initCrash()")
	}
	if rp := strings.Index(body, "crash.RecordAndRepanic(\"serve\")"); rp < 0 || rp > strings.Index(body, "config.Load()") {
		t.Fatal("cmdServe must defer crash.RecordAndRepanic before it does anything else")
	}
	for _, later := range []string{"config.Load()", "net.Listen(", "go srv", "go func"} {
		if i := strings.Index(body, later); i >= 0 && i < initAt {
			t.Fatalf("initCrash() must come before %s", later)
		}
	}
}
