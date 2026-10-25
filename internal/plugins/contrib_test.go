package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

const secretSentinel = "SENTINEL-s3cr3t-token-91f2"

type fakeSettings map[string]string

func (f fakeSettings) Setting(plugin, key string) (string, bool) {
	v, ok := f[plugin+"/"+key]
	return v, ok
}

const demoContributes = `"settings":[{"key":"token","type":"secret","label":"API token"}],` +
	`"contributes":{` +
	`"mcpServers":[{"name":"files","transport":"stdio","command":["go","run","./mcp"],"env":{"TOKEN":"${setting.token}","MODE":"plain"}},` +
	`{"name":"remote","transport":"http","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer ${setting.token}"}}],` +
	`"skills":["skills/review"],` +
	`"profiles":["profiles/reviewer.json","profiles/boss.json"]}`

func demoFiles() map[string]string {
	return map[string]string{
		"skills/review/SKILL.md":  "---\nname: review\ndescription: \"Use when reviewing a diff\"\nallowed-tools: Bash\n---\nLook at the diff.\n",
		"skills/review/notes.txt": "extra",
		"profiles/reviewer.json":  `{"name":"reviewer","provider":"claude-native","instructions":"Review.","skills":["demo:review"]}`,
		"profiles/boss.json":      `{"name":"boss","provider":"claude-native","permissionMode":"bypassPermissions","orchestrationTools":true,"childPolicy":"profiles","childProfiles":["reviewer"]}`,
	}
}

// enableDemo installs the demo plugin (or a variant) and enables it.
func enableDemo(t *testing.T, e *env, id string, extra string, files map[string]string) Info {
	t.Helper()
	if extra == "" {
		extra = demoContributes
	}
	if files == nil {
		files = demoFiles()
	}
	in := mustInstall(t, e, writePluginDir(t, manifestWith(id, "1.0.0", extra), files))
	out, err := e.m.Enable(id, in.Consent.Fingerprint)
	if err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	return out
}

func newContribEnv(t *testing.T, settings SettingsProvider, userProfiles ...string) *env {
	t.Helper()
	e := newEnv(t)
	e.m = New(Options{
		Home: e.home, Store: e.st, Version: "0.15.0", Enabled: func() bool { return *e.on },
		Settings: settings, UserProfileNames: func() []string { return userProfiles },
	})
	e.st.SetOverlay(e.m)
	return e
}

func mcpByName(rows []store.McpServer) map[string]store.McpServer {
	out := map[string]store.McpServer{}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func TestMcpContribution_SecretSubstitution_RedactedOnRead(t *testing.T) {
	e := newContribEnv(t, fakeSettings{"demo/token": secretSentinel})
	enableDemo(t, e, "demo", "", nil)

	// Read path: owned, namespaced, placeholders only.
	read := mcpByName(e.m.OwnedMcp())
	files, ok := read["demo:files"]
	if !ok || files.OwnerPlugin != "demo" || !store.IsOwnedID(files.ID) {
		t.Fatalf("owned read rows = %+v", read)
	}
	b, _ := json.Marshal(e.m.OwnedMcp())
	if strings.Contains(string(b), secretSentinel) {
		t.Fatalf("a read path carries the secret: %s", b)
	}
	if !strings.Contains(files.Env, "${setting.token}") {
		t.Fatalf("read env should keep the placeholder, got %s", files.Env)
	}
	// Plugin listings and consent never carry it either.
	l, _ := e.m.List()
	lb, _ := json.Marshal(l)
	if strings.Contains(string(lb), secretSentinel) {
		t.Fatalf("plugin.list carries the secret: %s", lb)
	}

	// Run path: substituted, only into env and headers, under the wire name.
	run := mcpByName(e.m.OwnedMcpForRun())
	rf, rr := run["demo__files"], run["demo__remote"]
	if !strings.Contains(rf.Env, secretSentinel) || !strings.Contains(rr.Headers, "Bearer "+secretSentinel) {
		t.Fatalf("run rows lack the substituted secret: %+v / %+v", rf, rr)
	}
	for _, s := range []string{rf.Command, rf.Args, rf.URL, rr.URL, rr.Command, rr.Args, rf.Name, rr.Name} {
		if strings.Contains(s, secretSentinel) {
			t.Fatalf("secret reached a field other than env/headers: %q", s)
		}
	}
}

func TestMcpContribution_MissingSetting_NotInjected(t *testing.T) {
	e := newContribEnv(t, fakeSettings{})
	enableDemo(t, e, "demo", "", nil)
	if len(e.m.OwnedMcp()) != 2 {
		t.Fatalf("both servers are still listed: %+v", e.m.OwnedMcp())
	}
	if run := e.m.OwnedMcpForRun(); len(run) != 0 {
		t.Fatalf("a server whose setting has no value must not be injected with an empty secret: %+v", run)
	}
	got, _ := e.m.Get("demo")
	if len(got.Conflicts) != 2 || !strings.Contains(got.Conflicts[0].Reason, "token") {
		t.Fatalf("conflicts should say which setting is unset: %+v", got.Conflicts)
	}
}

func TestManifest_SettingPlaceholderOnlyInEnvAndHeaders(t *testing.T) {
	for name, c := range map[string]string{
		"in command":  `"settings":[{"key":"t","type":"secret","label":"t"}],"contributes":{"mcpServers":[{"name":"a","transport":"stdio","command":["x","${setting.t}"]}]}`,
		"in url":      `"settings":[{"key":"t","type":"secret","label":"t"}],"contributes":{"mcpServers":[{"name":"a","transport":"http","url":"https://h/${setting.t}"}]}`,
		"undeclared":  `"contributes":{"mcpServers":[{"name":"a","transport":"stdio","command":["x"],"env":{"A":"${setting.nope}"}}]}`,
		"malformed":   `"settings":[{"key":"t","type":"secret","label":"t"}],"contributes":{"mcpServers":[{"name":"a","transport":"stdio","command":["x"],"env":{"A":"${setting.t"}}]}`,
		"env on http": `"contributes":{"mcpServers":[{"name":"a","transport":"http","url":"https://h","env":{"A":"1"}}]}`,
	} {
		if _, err := ParseManifest([]byte(manifestWith("demo", "1.0.0", c)), "0.15.0"); err == nil {
			t.Errorf("%s: manifest accepted", name)
		}
	}
}

func TestSkillContribution_ActiveOnly_AndDirNames(t *testing.T) {
	e := newContribEnv(t, nil)
	enableDemo(t, e, "demo", "", nil)
	sk := e.m.Skills()
	if len(sk) != 1 || sk[0].Qualified != "demo:review" || sk[0].Dir != "demo--review" || sk[0].Description != "Use when reviewing a diff" {
		t.Fatalf("skills = %+v", sk)
	}
	if _, ok := e.m.PluginSkill("demo:review"); !ok {
		t.Fatal("active plugin skill must resolve")
	}
	if _, ok := e.m.PluginSkill("demo:nope"); ok {
		t.Fatal("unknown skill must not resolve")
	}
}

func TestSkillFromDisabledPlugin_NotDelivered(t *testing.T) {
	for name, off := range map[string]func(*env){
		"disabled":   func(e *env) { _, _ = e.m.Disable("demo") },
		"removed":    func(e *env) { _ = e.m.Remove("demo", false) },
		"global off": func(e *env) { *e.on = false },
	} {
		t.Run(name, func(t *testing.T) {
			e := newContribEnv(t, nil)
			enableDemo(t, e, "demo", "", nil)
			if len(e.m.Skills()) != 1 {
				t.Fatal("precondition: skill listed while enabled")
			}
			off(e)
			if len(e.m.Skills()) != 0 || len(e.m.OwnedMcp()) != 0 || len(e.m.OwnedProfiles()) != 0 {
				t.Fatalf("contributions still visible: %d skills %d mcp %d profiles", len(e.m.Skills()), len(e.m.OwnedMcp()), len(e.m.OwnedProfiles()))
			}
			if _, ok := e.m.PluginSkill("demo:review"); ok {
				t.Fatal("the skill still resolves")
			}
		})
	}
}

func TestGlobalSwitchOff_NoContributionListed(t *testing.T) {
	e := newContribEnv(t, fakeSettings{"demo/token": secretSentinel})
	enableDemo(t, e, "demo", "", nil)
	*e.on = false
	if len(e.m.OwnedMcpForRun()) != 0 || len(e.m.OwnedMcp()) != 0 || len(e.m.Skills()) != 0 || len(e.m.OwnedProfiles()) != 0 {
		t.Fatal("with plugins.enabled false nothing is contributed (AC2)")
	}
	*e.on = true
	if len(e.m.OwnedMcp()) != 2 {
		t.Fatal("flipping the switch back applies with no restart")
	}
}

func TestProfileContribution_OwnedSeat_PermissionEscalationFlagged(t *testing.T) {
	e := newContribEnv(t, nil)
	in := mustInstall(t, e, writePluginDir(t, manifestWith("demo", "1.0.0", demoContributes), demoFiles()))

	var boss, reviewer *ProfileEntry
	for i := range in.Consent.Profiles {
		switch in.Consent.Profiles[i].Name {
		case "boss":
			boss = &in.Consent.Profiles[i]
		case "reviewer":
			reviewer = &in.Consent.Profiles[i]
		}
	}
	if boss == nil || reviewer == nil {
		t.Fatalf("consent lacks the profiles: %+v", in.Consent.Profiles)
	}
	joined := strings.Join(boss.Escalations, " | ")
	for _, want := range []string{"bypassPermissions", "orchestration tools", "child agents"} {
		if !strings.Contains(joined, want) {
			t.Errorf("boss escalations %q lack %q", joined, want)
		}
	}
	if len(reviewer.Escalations) != 0 {
		t.Errorf("a calm profile is not flagged: %v", reviewer.Escalations)
	}

	// Enabled: both are owned, namespaced, read-only and carry the seat fields.
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	ps := e.m.OwnedProfiles()
	if len(ps) != 2 {
		t.Fatalf("owned profiles = %+v", ps)
	}
	byName := map[string]store.AgentProfile{}
	for _, p := range ps {
		byName[p.Name] = p
	}
	b := byName["demo:boss"]
	if b.Owner == nil || *b.Owner != "demo" || !b.ReadOnly || b.ChildPolicy != "profiles" || b.OrchestrationTools == nil || !*b.OrchestrationTools || b.PermissionMode != "bypassPermissions" {
		t.Fatalf("boss seat = %+v", b)
	}
	if len(b.ChildProfileIDs) != 1 || b.ChildProfileIDs[0] != byName["demo:reviewer"].ID {
		t.Fatalf("childProfiles must resolve to the sibling's id: %+v", b.ChildProfileIDs)
	}
	got, err := e.st.GetAgentProfile(b.ID)
	if err != nil || got.Name != "demo:boss" {
		t.Fatalf("an owned id resolves through the store: %+v %v", got, err)
	}
	if _, err := e.st.ListAgentProfiles(); err != nil {
		t.Fatal(err)
	}
	if rows, _ := e.st.ListAgentProfiles(); len(rows) != 0 {
		t.Fatalf("owned profiles must never be written to the user's table: %+v", rows)
	}
	_, _ = e.m.Disable("demo")
	if _, err := e.st.GetAgentProfile(b.ID); err == nil {
		t.Fatal("a disabled plugin's profile must not resolve")
	}
}

func TestUpdate_AddsEscalatingProfile_StaysPending(t *testing.T) {
	e := newContribEnv(t, nil)
	calm := map[string]string{"profiles/a.json": `{"name":"a","provider":"claude-native"}`}
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"contributes":{"profiles":["profiles/a.json"]}`), calm)
	in := mustInstall(t, e, src)
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	// A calm second profile re-applies at once (no capability change).
	writeFile(t, filepath.Join(src, "profiles/b.json"), []byte(`{"name":"b","provider":"claude-native","permissionMode":"plan"}`))
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "1.1.0", `"contributes":{"profiles":["profiles/a.json","profiles/b.json"]}`)))
	res, err := e.m.Update(bg, "demo", "", false)
	if err != nil || !res.Applied || res.Pending {
		t.Fatalf("a calm profile is not a capability change: %+v %v", res, err)
	}
	// An escalating one stays pending; the old version keeps serving.
	writeFile(t, filepath.Join(src, "profiles/c.json"), []byte(`{"name":"c","provider":"claude-native","autoAccept":true}`))
	writeFile(t, filepath.Join(src, ManifestFile), []byte(manifestWith("demo", "1.2.0", `"contributes":{"profiles":["profiles/a.json","profiles/b.json","profiles/c.json"]}`)))
	res, err = e.m.Update(bg, "demo", "", false)
	if err != nil || !res.Pending || res.Applied {
		t.Fatalf("an escalating profile must wait for consent: %+v %v", res, err)
	}
	if got := len(e.m.OwnedProfiles()); got != 2 {
		t.Fatalf("the previous version keeps serving (2 profiles), got %d", got)
	}
	if res.Info.Pending == nil || len(res.Info.Pending.Consent.Profiles) != 3 {
		t.Fatalf("pending consent should show the new profile: %+v", res.Info.Pending)
	}
}

func TestNameCollision_CoreWins_ThenPluginIdOrder(t *testing.T) {
	// A user profile already named like an owned one wins; the owned row is
	// suppressed and the plugin says so.
	e := newContribEnv(t, nil, "demo:reviewer")
	enableDemo(t, e, "demo", "", nil)
	names := map[string]bool{}
	for _, p := range e.m.OwnedProfiles() {
		names[p.Name] = true
	}
	if names["demo:reviewer"] || !names["demo:boss"] {
		t.Fatalf("the user's profile wins the name: %v", names)
	}
	got, _ := e.m.Get("demo")
	var profileConflicts []Conflict
	for _, c := range got.Conflicts {
		if c.Kind == "profile" {
			profileConflicts = append(profileConflicts, c)
		}
	}
	if len(profileConflicts) != 1 || profileConflicts[0].Name != "reviewer" || !strings.Contains(profileConflicts[0].Reason, "already exists") {
		t.Fatalf("the suppression must be visible on the plugin: %+v", got.Conflicts)
	}
	// Two plugins list in id order and never collide: ids differ, so names do.
	one := `"contributes":{"profiles":["profiles/p.json"]}`
	pf := map[string]string{"profiles/p.json": `{"name":"shared","provider":"claude-native"}`}
	enableDemo(t, e, "zeta", one, pf)
	enableDemo(t, e, "alpha", one, pf)
	var order []string
	for _, p := range e.m.OwnedProfiles() {
		order = append(order, p.Name)
	}
	if want := "alpha:shared,demo:boss,zeta:shared"; !contains2(order, "alpha:shared", "zeta:shared") || strings.Join(order[:1], ",") != "alpha:shared" {
		t.Fatalf("plugin-id order, got %v want to start with alpha:shared (full: %s)", order, want)
	}
}

func contains2(list []string, a, b string) bool {
	var fa, fb bool
	for _, s := range list {
		fa, fb = fa || s == a, fb || s == b
	}
	return fa && fb
}

func TestNamespacing_NoShadowing(t *testing.T) {
	e := newContribEnv(t, nil)
	enableDemo(t, e, "demo", "", nil)
	seenID := map[int64]bool{}
	for _, m := range e.m.OwnedMcp() {
		if !strings.HasPrefix(m.Name, "demo:") || m.OwnerPlugin != "demo" || !store.IsOwnedID(m.ID) || seenID[m.ID] {
			t.Fatalf("owned mcp row not namespaced/unique/owned-range: %+v", m)
		}
		seenID[m.ID] = true
	}
	for _, p := range e.m.OwnedProfiles() {
		if !strings.HasPrefix(p.Name, "demo:") || !store.IsOwnedID(p.ID) || seenID[p.ID] {
			t.Fatalf("owned profile not namespaced/unique/owned-range: %+v", p)
		}
		seenID[p.ID] = true
	}
	for _, s := range e.m.Skills() {
		if !strings.HasPrefix(s.Qualified, "demo:") {
			t.Fatalf("owned skill not namespaced: %+v", s)
		}
	}
	// An owned row can never alias a user row: user ids stay below the base.
	u, err := e.st.CreateMcpServer(store.McpServer{Name: "files", Transport: "stdio", Command: "x", Args: "[]", Env: "{}"})
	if err != nil || store.IsOwnedID(u.ID) {
		t.Fatalf("user row id %d must stay out of the owned range (%v)", u.ID, err)
	}
	// And the wire name of an owned server cannot be a name a user can create.
	for _, m := range e.m.OwnedMcpForRun() {
		if strings.Contains(m.Name, ":") {
			t.Fatalf("run name %q still holds a colon", m.Name)
		}
	}
}

func TestProfileContribution_InvalidProfile_NotApplied(t *testing.T) {
	e := newContribEnv(t, nil)
	e.m = New(Options{
		Home: e.home, Store: e.st, Version: "0.15.0", Enabled: func() bool { return true },
		ValidateProfile: func(p store.AgentProfile) error {
			if p.Provider != "claude-native" {
				return fmt.Errorf("unknown provider %q", p.Provider)
			}
			return nil
		},
	})
	files := map[string]string{"profiles/x.json": `{"name":"x","provider":"nope"}`, "profiles/y.json": `{"name":"y","provider":"claude-native"}`}
	enableDemo(t, e, "demo", `"contributes":{"profiles":["profiles/x.json","profiles/y.json"]}`, files)
	ps := e.m.OwnedProfiles()
	if len(ps) != 1 || ps[0].Name != "demo:y" {
		t.Fatalf("only the valid profile is applied: %+v", ps)
	}
	got, _ := e.m.Get("demo")
	if len(got.Conflicts) != 1 || !strings.Contains(got.Conflicts[0].Reason, "unknown provider") {
		t.Fatalf("the rejection is visible: %+v", got.Conflicts)
	}
}

func TestContributions_BadFilesRejectedAtInstall(t *testing.T) {
	bad := map[string]struct {
		extra string
		files map[string]string
		want  string
	}{
		"skill without SKILL.md": {`"contributes":{"skills":["skills/x"]}`, map[string]string{"skills/x/readme.md": "x"}, "SKILL.md"},
		"skill bad name":         {`"contributes":{"skills":["skills/Bad_Name"]}`, map[string]string{"skills/Bad_Name/SKILL.md": "x"}, "skill name"},
		"profile unknown field":  {`"contributes":{"profiles":["p.json"]}`, map[string]string{"p.json": `{"name":"a","provider":"x","sudo":true}`}, "not a valid profile"},
		"profile bad child":      {`"contributes":{"profiles":["p.json"]}`, map[string]string{"p.json": `{"name":"a","provider":"x","childPolicy":"profiles","childProfiles":["ghost"]}`}, "ghost"},
		"profile fallback":       {`"contributes":{"profiles":["p.json"]}`, map[string]string{"p.json": `{"name":"a","provider":"x","onRateLimit":"fallback"}`}, "fallback"},
		"profile bad skill":      {`"contributes":{"profiles":["p.json"]}`, map[string]string{"p.json": `{"name":"a","provider":"x","skills":["a:b:c"]}`}, "skill"},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			e := newContribEnv(t, nil)
			_, err := e.m.Install(bg, writePluginDir(t, manifestWith("demo", "1.0.0", c.extra), c.files))
			var me *ManifestError
			if !errors.As(err, &me) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a manifest error mentioning %q, got %v", c.want, err)
			}
			if _, gerr := e.st.GetPlugin("demo"); gerr == nil {
				t.Fatal("a rejected plugin must leave no row")
			}
		})
	}
}

func TestContributions_SymlinkInSkillRejected(t *testing.T) {
	e := newContribEnv(t, nil)
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"contributes":{"skills":["skills/x"]}`), map[string]string{"skills/x/SKILL.md": "ok"})
	if err := os.Symlink("/etc/hosts", filepath.Join(src, "skills/x/leak.txt")); err != nil {
		t.Skip("no symlinks")
	}
	if _, err := e.m.Install(bg, src); err == nil {
		t.Fatal("a symlink anywhere in the plugin must be rejected")
	}
}

func TestLinkWarning_OnLinkedConsent(t *testing.T) {
	e := newContribEnv(t, nil)
	src := writePluginDir(t, manifestWith("demo", "1.0.0", `"contributes":{"skills":["skills/x"]}`), map[string]string{"skills/x/SKILL.md": "ok"})
	in, err := e.m.Link(bg, src)
	if err != nil {
		t.Fatal(err)
	}
	if in.Consent.LinkWarning != LinkWarningText || in.Consent.SourceKind != "link" {
		t.Fatalf("linked consent = %+v", in.Consent)
	}
	cp := mustInstall(t, e, writePluginDir(t, manifestWith("copy", "1.0.0", ""), nil))
	if cp.Consent.LinkWarning != "" {
		t.Fatal("a copied plugin carries no link warning")
	}
}

func TestFingerprint_StableWithoutProfiles(t *testing.T) {
	// Data contributions must not change the fingerprint of a plugin that
	// declares none, or every plugin installed before would ask for consent
	// again: it must equal the PA.1 formula.
	m, err := ParseManifest([]byte(manifestWith("demo", "1.0.0", `"capabilities":["tasks.read"],"contributes":{"mcpServers":[{"name":"s","transport":"stdio","command":["x"]}]}`)), "0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	c := BuildConsent(m, Source{Kind: SourceDir, Spec: "/x"}, "h")
	old, _ := json.Marshal(struct {
		Process      []string
		Capabilities []string
		HookEvents   []HookEvent
		McpServers   []McpEntry
		AcpProviders []AcpEntry
	}{c.Process, c.Capabilities, c.HookEvents, c.McpServers, c.AcpProviders})
	if want := sum256(old); c.Fingerprint != want {
		t.Fatalf("fingerprint %s differs from the PA.1 formula %s", c.Fingerprint, want)
	}
}
