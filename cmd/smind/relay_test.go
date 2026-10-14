package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/relay/server"
)

// runRelay runs cmdRelay with args against a fresh SMIND_HOME and returns
// its exit code, stdout and stderr.
func runRelay(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out string
	errOut := captureStderr(t, func() {
		out = captureStdout(t, func() { code = cmdRelay(args) })
	})
	return code, out, errOut
}

// relayHome points SMIND_HOME at a temp dir so host commands use a private
// relay store.
func relayHome(t *testing.T) {
	t.Helper()
	t.Setenv("SMIND_HOME", t.TempDir())
}

func TestRelayHostNewAliasesWorkspaceNew(t *testing.T) {
	relayHome(t)
	code, out, _ := runRelay(t, "host", "new", "a")
	if code != 0 || !strings.Contains(out, `relay host "a" enrolled`) {
		t.Fatalf("host new = %d %q", code, out)
	}
	code, out, _ = runRelay(t, "workspace", "new", "b")
	if code != 0 || !strings.Contains(out, `relay host "b" enrolled`) {
		t.Fatalf("workspace new = %d %q", code, out)
	}
	ids, err := server.ListHosts(server.RelayDir())
	if err != nil || len(ids) != 2 {
		t.Fatalf("ListHosts = %v, %v; want both a and b", ids, err)
	}
}

func TestRelayWorkspaceAliasPrintsDeprecationToStderr(t *testing.T) {
	relayHome(t)
	_, out, errOut := runRelay(t, "workspace", "ls")
	if strings.Contains(out, "deprecated") {
		t.Fatalf("deprecation note leaked to stdout: %q", out)
	}
	if lines := strings.Split(strings.TrimSpace(errOut), "\n"); len(lines) != 1 ||
		!strings.Contains(errOut, "deprecated") || !strings.Contains(errOut, "smind relay host") {
		t.Fatalf("stderr = %q, want a one-line deprecation notice naming `smind relay host`", errOut)
	}
	_, _, errOut = runRelay(t, "host", "ls")
	if errOut != "" {
		t.Fatalf("`relay host ls` wrote to stderr: %q", errOut)
	}
}

func TestRelayHostLsMatchesWorkspaceLs(t *testing.T) {
	relayHome(t)
	if _, err := server.EnrollHost(server.RelayDir(), "zeta"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.EnrollHost(server.RelayDir(), "alpha"); err != nil {
		t.Fatal(err)
	}
	hostCode, hostOut, _ := runRelay(t, "host", "ls")
	wsCode, wsOut, _ := runRelay(t, "workspace", "ls")
	if hostCode != wsCode || hostOut != wsOut || hostOut != "alpha\nzeta\n" {
		t.Fatalf("host ls = %d %q, workspace ls = %d %q", hostCode, hostOut, wsCode, wsOut)
	}
}

func TestRelayHostRmRemovesStoreEntry(t *testing.T) {
	relayHome(t)
	for _, id := range []string{"keep", "drop"} {
		if _, err := server.EnrollHost(server.RelayDir(), id); err != nil {
			t.Fatal(err)
		}
	}
	if code, out, _ := runRelay(t, "host", "rm", "drop"); code != 0 || !strings.Contains(out, `"drop" removed`) {
		t.Fatalf("host rm = %d %q", code, out)
	}
	ids, err := server.ListHosts(server.RelayDir())
	if err != nil || len(ids) != 1 || ids[0] != "keep" {
		t.Fatalf("ListHosts after rm = %v, %v; want [keep]", ids, err)
	}
	if err := server.RemoveHost(server.RelayDir(), "drop"); err == nil {
		t.Fatal("removing an unknown relay host must fail")
	}
}

func TestRelayHostRotateInvalidatesOldSecret(t *testing.T) {
	relayHome(t)
	old, err := server.EnrollHost(server.RelayDir(), "h")
	if err != nil {
		t.Fatal(err)
	}
	code, out, _ := runRelay(t, "host", "rotate", "h")
	if code != 0 {
		t.Fatalf("host rotate = %d %q", code, out)
	}
	rotated, err := server.RotateHost(server.RelayDir(), "h")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(old, rotated) {
		t.Fatal("rotate returned the old secret")
	}
	// The CLI's printed secret is a fresh 32-byte hex secret, shown once.
	if !strings.Contains(out, "shown ONCE") || strings.Contains(out, secretHex(old)) {
		t.Fatalf("rotate output = %q", out)
	}
	if _, err := server.RotateHost(server.RelayDir(), "missing"); err == nil {
		t.Fatal("rotating an unknown relay host must fail")
	}
}

const relayHostSentence = "Connecting registers this daemon once with the relay; every paired client sees all of this daemon's workspaces, spaces and tasks; nothing is created in smind."

func TestRelayHelpSaysRelayHostAndAllWorkspaces(t *testing.T) {
	var buf bytes.Buffer
	printRelayUsage(&buf)
	help := buf.String()
	if !strings.Contains(help, relayHostSentence) {
		t.Fatalf("relay help lacks the sentence %q:\n%s", relayHostSentence, help)
	}
	for _, want := range []string{"smind relay host new <id>", "smind relay host ls", "smind relay host rm <id>", "smind relay host rotate <id>", "<host-id>"} {
		if !strings.Contains(help, want) {
			t.Errorf("relay help lacks %q", want)
		}
	}
	if !strings.Contains(relayConnectNote, "nothing is created in smind") || relayConnectNote != relayHostSentence {
		t.Errorf("relayConnectNote = %q, want the shared sentence", relayConnectNote)
	}
}

func TestNoUserVisibleStringCallsRelayRecordAWorkspace(t *testing.T) {
	var relayHelp, mainHelp bytes.Buffer
	printRelayUsage(&relayHelp)
	printUsage(&mainHelp)
	// Every "workspace" in the relay help is smind's own concept or the
	// deprecated-alias note; none names a relay record.
	for _, line := range strings.Split(relayHelp.String(), "\n") {
		for _, bad := range []string{"enroll a workspace", "enrolled workspaces", "authenticates workspaces", "workspace secret", "<workspace-id>", "relay workspace new", "relay workspace ls"} {
			if strings.Contains(line, bad) {
				t.Errorf("relay help calls a relay record a workspace: %q", line)
			}
		}
	}
	for _, line := range strings.Split(mainHelp.String(), "\n") {
		if strings.Contains(line, "smind relay") && strings.Contains(line, "workspace") {
			t.Errorf("main help calls a relay record a workspace: %q", line)
		}
	}

	// Source scan: the CLI sources' user-facing relay strings.
	src, err := os.ReadFile("relay.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`"usage: smind relay workspace`, `<workspace-id>`, `(workspace %q`, `no workspaces enrolled`, `workspace %q enrolled`} {
		if strings.Contains(string(src), bad) {
			t.Errorf("cmd/smind/relay.go still contains the user-visible string %q", bad)
		}
	}
}
