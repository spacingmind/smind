package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type fixedRoots []string

func (f fixedRoots) SessionRoots(string, string) []string { return f }

func (e *hcEnv) doctorSessions(t *testing.T, rc *requestContext, roots []string) (doctorSessionsResult, string) {
	t.Helper()
	out, err := handleDoctorSessions(e.db, fixedRoots(roots))(context.Background(), rc, nil)
	if err != nil {
		t.Fatalf("doctor.sessions: %v", err)
	}
	b, _ := json.Marshal(out)
	return out.(doctorSessionsResult), string(b)
}

// doctor.sessions counts, with the rebuild's own path rules, how many runs still
// have the provider session the finalizer recorded; it prints counts, never a
// path or a run id.
func TestDoctorSessionsCountsReadableTooLargeAndGone(t *testing.T) {
	e := newHCEnv(t) // run "skel": skeleton form, native_ref -> the fixture session under e.root
	local := &requestContext{local: true, channel: ChannelLoopback}

	res, raw := e.doctorSessions(t, local, []string{e.root})
	if res.Runs != 1 || res.WithRef != 1 || res.Readable != 1 || res.TooLarge != 0 || res.Gone != 0 {
		t.Fatalf("with the session in place: %+v", res)
	}
	for _, leak := range []string{e.root, "skel", "projects", hcSession} {
		if strings.Contains(raw, leak) {
			t.Fatalf("the result leaks %q: %s", leak, raw)
		}
	}

	// the CLI deleted it: gone
	data, _ := os.ReadFile(e.file)
	if err := os.Remove(e.file); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.doctorSessions(t, local, []string{e.root}); res.Gone != 1 || res.Readable != 0 {
		t.Fatalf("after the delete: %+v", res)
	}

	// over the size bound: there, but a rebuild would refuse it
	f, err := os.Create(e.file)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(65 << 20); err != nil {
		t.Skip("sparse file unavailable")
	}
	f.Close()
	if res, _ := e.doctorSessions(t, local, []string{e.root}); res.TooLarge != 1 || res.Readable != 0 || res.Gone != 0 {
		t.Fatalf("a 65 MiB session: %+v", res)
	}
	if err := os.WriteFile(e.file, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// a root the daemon would not read from is not "readable" either
	if res, _ := e.doctorSessions(t, local, []string{t.TempDir()}); res.Readable != 0 || res.Gone != 1 {
		t.Fatalf("session outside the allowed roots: %+v", res)
	}
	// a run with no recorded session is not counted as withRef
	if err := e.db.SetRunNativeRef("skel", ""); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.doctorSessions(t, local, []string{e.root}); res.Runs != 1 || res.WithRef != 0 {
		t.Fatalf("no native_ref: %+v", res)
	}
}

// The probe never opens a session file: it must work on a file smind could not
// read (mode 000) and must not change it.
func TestDoctorSessionsOnlyResolvesAndStats(t *testing.T) {
	e := newHCEnv(t)
	before, _ := os.Stat(e.file)
	local := &requestContext{local: true, channel: ChannelLoopback}
	if res, _ := e.doctorSessions(t, local, []string{e.root}); res.Readable != 1 {
		t.Fatalf("%+v", res)
	}
	after, _ := os.Stat(e.file)
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("the probe changed the session file")
	}
}

func TestDoctorSessionsIsLocalHumanOnly(t *testing.T) {
	e := newHCEnv(t)
	h := handleDoctorSessions(e.db, fixedRoots([]string{e.root}))
	for name, rc := range map[string]*requestContext{
		"nil": nil, "agent": {runScoped: true, local: true, channel: ChannelLoopback},
		"relay": {local: false, channel: ChannelLoopback}, "plain": {local: true, channel: ChannelPlain},
	} {
		if res, err := h(context.Background(), rc, nil); err == nil || res != nil {
			t.Errorf("%s answered (%v, %v), want forbidden", name, res, err)
		}
	}
}
