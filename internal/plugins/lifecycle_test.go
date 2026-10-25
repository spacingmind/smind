package plugins

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdate_CapabilityChange_StaysPending_PreviousKeepsRunning(t *testing.T) {
	e := newEnv(t)
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"capabilities":["tasks.read"]`), map[string]string{"v.txt": "one"})
	in := mustInstall(t, e, src)
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.home, "plugins", "demo", "data", "keep.txt"), []byte("state"))

	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "2.0.0", `"capabilities":["tasks.read","tasks.write"],"process":{"command":["node","x.js"]}`)))
	writeFile(t, filepath.Join(src, "v.txt"), []byte("two"))
	res, err := e.m.Update(bg, "demo", "", false)
	if err != nil || !res.Pending || res.Applied {
		t.Fatalf("%+v %v", res, err)
	}
	got, _ := e.m.Get("demo")
	if got.Version != "1.0.0" || !got.Enabled || !got.Active || got.Pending == nil || got.Pending.Consent.Version != "2.0.0" {
		t.Fatalf("previous version must keep serving: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "v.txt")); string(b) != "one" {
		t.Fatalf("live files changed before consent: %q", b)
	}

	// A pending tree edited after staging must not go live.
	pend := filepath.Join(e.home, "plugins", ".pending", "demo", "v.txt")
	os.WriteFile(pend, []byte("tampered"), 0o644)
	if _, err := e.m.Update(bg, "demo", "", true); CodeOf(err) != CodeHashMismatch {
		t.Fatalf("tampered pending applied: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "v.txt")); string(b) != "one" {
		t.Fatal("tampered update went live")
	}
	if res, err = e.m.Update(bg, "demo", "", false); err != nil || !res.Pending {
		t.Fatalf("%+v %v", res, err)
	}
	res, err = e.m.Update(bg, "demo", "", true)
	if err != nil || !res.Applied || res.Info.Version != "2.0.0" || res.Info.Pending != nil {
		t.Fatalf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "v.txt")); string(b) != "two" {
		t.Fatal("update not applied")
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "data", "keep.txt")); string(b) != "state" {
		t.Fatal("data/ did not survive the update")
	}
	if !res.Info.Enabled || !res.Info.Consented {
		t.Fatalf("an applied update keeps the plugin enabled and consented: %+v", res.Info)
	}
	if _, err := e.m.Update(bg, "demo", "", true); CodeOf(err) != CodeNoPending {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(e.home, "plugins", ".pending")); err == nil {
		t.Fatal("pending dir left behind")
	}

	// An update that changes no consent-relevant field applies at once.
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "2.1.0", `"capabilities":["tasks.write","tasks.read"],"process":{"command":["node","x.js"]}`)))
	res, err = e.m.Update(bg, "demo", "", false)
	if err != nil || !res.Applied || res.Info.Version != "2.1.0" || !res.Info.Active {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestUpdate_Failed_KeepsLastGood(t *testing.T) {
	e := newEnv(t)
	src := writePluginDir(t, manifestWith("demo", "1.0.0", ""), map[string]string{"v.txt": "one"})
	in := mustInstall(t, e, src)
	e.m.Enable("demo", in.Consent.Fingerprint)

	writeFile(t, filepath.Join(src, ManifestFile), []byte(`{"id":"demo"}`))
	if _, err := e.m.Update(bg, "demo", "", false); CodeOf(err) != CodeBadManifest {
		t.Fatalf("%v", err)
	}
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("other", "2.0.0", "")))
	if _, err := e.m.Update(bg, "demo", "", false); CodeOf(err) != CodeBadManifest {
		t.Fatalf("%v", err)
	}
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "3.0.0", "")))
	e.m.failAt = func(p string) error {
		if p == "swap" {
			return errors.New("boom")
		}
		return nil
	}
	if _, err := e.m.Update(bg, "demo", "", false); err == nil {
		t.Fatal("injected failure ignored")
	}
	e.m.failAt = nil
	got, _ := e.m.Get("demo")
	if got.Version != "1.0.0" || !got.Active || got.LastError == "" {
		t.Fatalf("the last good version must stay active and show its error: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "v.txt")); string(b) != "one" {
		t.Fatal("files changed")
	}
	if ents, _ := os.ReadDir(filepath.Join(e.home, "plugins")); len(ents) != 1 {
		t.Fatalf("leftovers in plugins/: %v", ents)
	}
}

func TestRemove_KeepData_AndLinkNeverDeleted(t *testing.T) {
	e := newEnv(t)

	in := mustInstall(t, e, writePluginDir(t, goodManifest, nil))
	writeFile(t, filepath.Join(e.home, "plugins", "demo", "data", "s.txt"), []byte("state"))
	e.m.Enable("demo", in.Consent.Fingerprint)
	if err := e.m.Remove("demo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.home, "plugins", "demo")); err == nil {
		t.Fatal("files left")
	}
	if _, err := e.st.GetPlugin("demo"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("row left")
	}

	mustInstall(t, e, writePluginDir(t, goodManifest, nil))
	writeFile(t, filepath.Join(e.home, "plugins", "demo", "data", "s.txt"), []byte("state"))
	if err := e.m.Remove("demo", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "data", "s.txt")); string(b) != "state" {
		t.Fatal("data/ not kept")
	}
	if _, err := os.Stat(filepath.Join(e.home, "plugins", "demo", ManifestFile)); err == nil {
		t.Fatal("code files kept")
	}
	mustInstall(t, e, writePluginDir(t, goodManifest, nil))
	if b, _ := os.ReadFile(filepath.Join(e.home, "plugins", "demo", "data", "s.txt")); string(b) != "state" {
		t.Fatal("reinstall did not adopt the kept data/")
	}
	e.m.Remove("demo", false)

	src := writePluginDir(t, goodManifest, map[string]string{"important.txt": "mine"})
	lin, err := e.m.Link(bg, src)
	if err != nil || lin.SourceKind != "link" {
		t.Fatalf("%+v %v", lin, err)
	}
	if _, err := os.Stat(filepath.Join(e.home, "plugins", "demo", "important.txt")); err == nil {
		t.Fatal("a linked source was copied")
	}
	if err := e.m.Remove("demo", false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{ManifestFile, "important.txt"} {
		if _, err := os.Stat(filepath.Join(src, f)); err != nil {
			t.Fatalf("remove deleted linked source file %s", f)
		}
	}
	// A data/ that became a symlink into the source must not be followed.
	if _, err := e.m.Link(bg, src); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(e.home, "plugins", "demo", "data"))
	if err := os.Symlink(src, filepath.Join(e.home, "plugins", "demo", "data")); err == nil {
		if err := e.m.Remove("demo", false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(src, "important.txt")); err != nil {
			t.Fatal("remove followed a data/ symlink into the source")
		}
	}
	if _, err := e.m.Get("ghost"); CodeOf(err) != CodeNotFound {
		t.Fatal(err)
	}
}

func TestReload_And_Start_Revalidate(t *testing.T) {
	e := newEnv(t)
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"capabilities":["tasks.read"]`), nil)
	lin, err := e.m.Link(bg, src)
	if err != nil {
		t.Fatal(err)
	}
	e.m.Enable("demo", lin.Consent.Fingerprint)

	writeFile(t, filepath.Join(src, ManifestFile), []byte(`{"id":"demo"}`))
	if _, err := e.m.Reload("demo"); err == nil {
		t.Fatal("broken manifest reloaded")
	}
	got, _ := e.m.Get("demo")
	if !got.Enabled || got.LastError == "" || got.Version != "1.0.0" {
		t.Fatalf("a failed reload keeps the last good state: %+v", got)
	}
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "1.1.0", `"capabilities":["tasks.read","tasks.write"]`)))
	got, err = e.m.Reload("demo")
	if err != nil || got.Enabled || got.Version != "1.1.0" || !strings.Contains(got.LastError, "again") {
		t.Fatalf("widened capabilities must disable until re-consent: %+v %v", got, err)
	}

	cp := mustInstall(t, e, writePluginDir(t, manifestWith("copy", "1.0.0", ""), map[string]string{"a.txt": "a"}))
	e.m.Enable("copy", cp.Consent.Fingerprint)
	os.WriteFile(filepath.Join(e.home, "plugins", "copy", "a.txt"), []byte("tampered"), 0o644)
	e.m.Start()
	got, _ = e.m.Get("copy")
	if got.Enabled || !strings.Contains(got.LastError, "changed") {
		t.Fatalf("a plugin edited after consent must be disabled at start: %+v", got)
	}
	os.MkdirAll(filepath.Join(e.home, "plugins", ".staging", "s-1", "tree"), 0o755)
	e.m.Start()
	if _, err := os.Stat(filepath.Join(e.home, "plugins", ".staging")); err == nil {
		t.Fatal("staging leftovers kept")
	}
}

func TestInstall_RejectsReservedDataAndBadLayouts(t *testing.T) {
	e := newEnv(t)
	_, err := e.m.Install(bg, writePluginDir(t, goodManifest, map[string]string{"data/x": "x"}))
	wantCode(t, err, CodeUnsafeEntry)

	_, err = e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", `"contributes":{"skills":["skills/missing"]}`), nil))
	wantCode(t, err, CodeBadManifest)

	d3 := t.TempDir()
	real := writeFile(t, filepath.Join(t.TempDir(), "m.json"), []byte(goodManifest))
	if err := os.Symlink(real, filepath.Join(d3, ManifestFile)); err == nil {
		_, err = e.m.Install(bg, d3)
		wantCode(t, err, CodeLinkRejected)
	}
	arch := buildTar(t, false, []ent{{name: "sub/smind-plugin.json", body: goodManifest}})
	p := writeFile(t, filepath.Join(t.TempDir(), "a.tar"), arch)
	_, err = e.m.Install(bg, p+"#sha256="+sum256(arch))
	wantCode(t, err, CodeBadManifest)

	d4 := writePluginDir(t, "{}", nil)
	os.WriteFile(filepath.Join(d4, ManifestFile), []byte(`{"id":"x","pad":"`+strings.Repeat("a", MaxManifestBytes)+`"}`), 0o644)
	_, err = e.m.Install(bg, d4)
	wantCode(t, err, CodeBadManifest)

	// A stale directory with real files in it is not silently overwritten.
	stale := filepath.Join(e.home, "plugins", "demo")
	writeFile(t, filepath.Join(stale, "junk.txt"), []byte("x"))
	_, err = e.m.Install(bg, writePluginDir(t, goodManifest, nil))
	wantCode(t, err, CodeLayout)
	if _, serr := os.Stat(filepath.Join(stale, "junk.txt")); serr != nil {
		t.Fatal("the stale directory was modified")
	}
}
