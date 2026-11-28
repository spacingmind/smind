package crash

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// goSitePolicy is the policy of every crash.Go / GoThen / GoFatal call site in
// the daemon packages, with the reason. The question asked of each goroutine:
// "was its panic fatal before, and is hiding it now harmless?". A panic that
// used to kill the daemon stays fatal (GoFatal: recorded, then it dies as
// before) unless losing the goroutine is harmless or a compensating action
// makes the loss visible (Go: contained and logged; GoThen: contained, with
// the action). A new site must be added here, with a reason.
var goSitePolicy = map[string]sitePolicy{
	"wsapi.dispatch":                   {"contain", "one request: dispatch's own recover answers the caller with an internal error and the connection lives; this wrapper is a net"},
	"runs.drive":                       {"contain", "drive's own recover records and finishes the run as an error; this wrapper is a net"},
	"quota.fetch":                      {"contain", "one-shot usage fetch: a panic leaves the cached usage stale, as the recover() the poller already had; logged"},
	"quota.spreadSkip":                 {"contain", "ends the post-resume spread wait when a forced refresh joins the flight: it only closes a context; a panic cannot touch usage state, logged"},
	"quota.providerFetch":              {"contain", "one-shot provider usage fetch: a panic leaves the cached usage stale; logged"},
	"quota.refreshAll":                 {"contain", "one-shot refresh fan-out: a panic leaves the cached usage stale; logged"},
	"wsapi.usageFetch":                 {"contain", "one-shot usage fetch behind a request: a panic leaves the cache stale; logged"},
	"wsapi.browserResolve":             {"contain", "one DNS lookup for a linked peer's name on the live deny list: a panic is already a failed lookup (the last good answer stays, a never-resolved name fails the browser closed) and the flight is released by a defer; logged"},
	"wsapi.browserKillSwitchWatch":     {"contain", "polls the kill switch every two seconds: a panic stops the watcher, and the per-call and settings-change revocations still apply; logged"},
	"wsapi.providerUsageFetch":         {"contain", "one-shot provider usage fetch behind a request: a panic leaves the cache stale; logged"},
	"wsapi.providerCLILookup":          {"contain", "one-shot provider CLI lookup behind a cache: a panic leaves the answer unset; logged"},
	"wsapi.pumpEvents":                 {"contain+compensate", "closes the connection, so the client sees the drop instead of a silently dead event stream"},
	"runs.forwardEvents":               {"contain+compensate", "cancels the run and drains events, so the runner cannot block and drive finishes the run"},
	"smind.serveListener":              {"die", "the daemon's front door: a contained panic would leave the daemon up with nothing listening or bridged"},
	"smind.relayBridge":                {"die", "the daemon's front door: a contained panic would leave the daemon up with nothing listening or bridged"},
	"accounts.oauthServe":              {"die", "the daemon's front door: a contained panic would leave the daemon up with nothing listening or bridged"},
	"gitstate.heartbeatLoop":           {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"push.run":                         {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"quota.passiveRun":                 {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"runs.pruneLoop":                   {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"server.requestLogWrite":           {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"smind.automationRun":              {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"smind.finalizePending":            {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"smind.probeJanitor":               {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"smind.scratchSweep":               {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"smind.stallDetector":              {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.fleetIdemSweeper":           {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.inboxLoop":                  {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.pluginWatch":                {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.policySweeper":              {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.stewardPassWatch":           {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.stewardSeatWatch":           {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.validationWatch":            {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"wsapi.usageDrain":                 {"die", "background loop or sweeper: contained, its subsystem would stop silently; dying lets the supervisor restart it"},
	"taskrunner.acpModeProbe":          {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"taskrunner.forwardACPUpdates":     {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"taskrunner.forwardClaudeMessages": {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"taskrunner.forwardCodexUpdates":   {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"taskrunner.isolatedForward":       {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"taskrunner.probeForward":          {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"taskrunner.replayCollect":         {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"terminal.checkpointLoop":          {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"terminal.close":                   {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"terminal.readLoop":                {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"terminal.relay":                   {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"terminal.waitLoop":                {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"runs.storedRelay":                 {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"runs.subscribeRelay":              {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"wsapi.pluginPump":                 {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"wsapi.forgeRead":                  {"die", "per-session or per-run pump or forwarder: contained, that session or run would hang with no signal to anyone"},
	"gitstate.fetch":                   {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"gitstate.invalidate":              {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"gitstate.listWorker":              {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"runs.deliverOne":                  {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"runs.evaluateChildNotifications":  {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"runs.stopRun":                     {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"wsapi.stewardAutoReview":          {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"wsapi.stewardRecheck":             {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"wsapi.stewardRecheckPasses":       {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"workspace.setupRun":               {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
	"smind.mcpWaitRuns":                {"die", "state-changing worker: contained, the half-done state (a busy flag, a pending review, a queue slot, a setup) would be left behind"},
}

type sitePolicy struct{ kind, why string }

// kindOfHelper maps the crash helper a site calls to its policy kind.
var kindOfHelper = map[string]string{"Go": "contain", "GoThen": "contain+compensate", "GoFatal": "die"}

// TestCrashGoSitesHavePolicy: crash-go-sites-have-policy: every call site is
// in goSitePolicy with the kind that matches the helper it uses, every entry
// has a call site, and every reason is written.
func TestCrashGoSitesHavePolicy(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string]string{} // site -> kind from the helper used
	for _, dir := range scannedDaemonPackages {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(root, dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "crash" {
					return true
				}
				kind, ok := kindOfHelper[sel.Sel.Name]
				if !ok {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s: crash.%s needs a string literal site", fset.Position(call.Pos()), sel.Sel.Name)
					return true
				}
				site, _ := strconv.Unquote(lit.Value)
				if prev, dup := found[site]; dup {
					t.Errorf("%s: site %q is used twice (a report could not tell the two apart; prev kind %s)", fset.Position(call.Pos()), site, prev)
				}
				found[site] = kind
				return true
			})
		}
	}
	for site, kind := range found {
		p, ok := goSitePolicy[site]
		if !ok {
			t.Errorf("site %q (%s) has no entry in goSitePolicy: decide contain / contain+compensate / die and say why", site, kind)
			continue
		}
		if p.kind != kind {
			t.Errorf("site %q: goSitePolicy says %s but the code uses the %s helper", site, p.kind, kind)
		}
	}
	for site, p := range goSitePolicy {
		if _, ok := found[site]; !ok {
			t.Errorf("stale goSitePolicy entry %q (no call site)", site)
		}
		if strings.TrimSpace(p.why) == "" {
			t.Errorf("site %q has no reason", site)
		}
	}
}
