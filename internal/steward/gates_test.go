package steward

import (
	"strings"
	"testing"
)

func headSHA() string { return strings.Repeat("a", 40) }

// base is a clean, fully passing Inputs: one Go file inside the write scope,
// contract + passing validation at head, green CI, tidy PR on develop.
func base() Inputs {
	return Inputs{
		Task: TaskInfo{
			ID:      7,
			Title:   "digest gates",
			Branch:  "smind/task-x",
			HeadSHA: headSHA(),
			BaseSHA: strings.Repeat("b", 40),
		},
		Numstat:     "3\t1\tinternal/steward/a.go\n",
		HasContract: true,
		WriteScope:  []string{"internal/steward/"},
		PlanFile:    "docs/plans/active/steward-seat.md",
		Stacking:    StackInfo{BaseReachableFromDevelop: true},
		PR: &PRInfo{
			Number: 12,
			Base:   "develop",
			Title:  "feat(steward): x",
			Body:   "## Validated\nx\n## Not done\ny",
		},
		CI:         CIInfo{State: "success"},
		Validation: &ValidationInfo{ID: 1, Status: "pass", CommitSHA: headSHA(), MergeReady: true},
	}
}

func mustGate(t *testing.T, in Inputs, id string) Gate {
	t.Helper()
	g, ok := GateByID(ComputeGates(in), id)
	if !ok {
		t.Fatalf("gate %q not found", id)
	}
	return g
}

func framesAt(sha string) []FrameInfo {
	var out []FrameInfo
	for _, cell := range frameMatrix {
		out = append(out, FrameInfo{
			Path:        "frame-" + cell.Theme + ".png",
			SizeBytes:   1234,
			CapturedSHA: sha,
			Viewport:    cell.Viewport,
			Theme:       cell.Theme,
		})
	}
	return out
}

func TestGates_AllPassOnCleanInputs(t *testing.T) {
	gs := ComputeGates(base())
	if len(gs) != len(GateIDs) || len(GateIDs) != 11 {
		t.Fatalf("got %d gates, want %d", len(gs), len(GateIDs))
	}
	for i, g := range gs {
		if g.ID != GateIDs[i] {
			t.Errorf("gate %d id = %q, want %q", i, g.ID, GateIDs[i])
		}
		if g.State != GatePass && g.State != GateNA {
			t.Errorf("gate %s state = %q (%s), want pass or n/a", g.ID, g.State, g.Detail)
		}
	}
	if g, _ := GateByID(gs, "frames_fresh"); g.State != GateNA {
		t.Errorf("frames_fresh = %q, want n/a (no web/ or mobile/ change)", g.State)
	}
	if g, _ := GateByID(gs, "peer_claims"); g.State != GateNA {
		t.Errorf("peer_claims = %q, want n/a (no handoffs)", g.State)
	}
}

func TestDigest_BinaryDetectedFromNumstat(t *testing.T) {
	in := base()
	in.Numstat = "1\t1\tinternal/steward/a.go\n-\t-\tlogo.png\n"
	g := mustGate(t, in, "no_binaries")
	if g.State != GateFail {
		t.Fatalf("no_binaries = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "logo.png") {
		t.Errorf("detail %q does not name logo.png", g.Detail)
	}
}

func TestDigest_ChurnGenSchemasLockfile(t *testing.T) {
	cases := []struct {
		name    string
		numstat string
		want    string
	}{
		{"generated schemas", "1\t1\tinternal/gen/schemas/x.json\n", GateFail},
		{"build artifact in dist", "1\t1\tweb/packages/ui/dist/a.js\n", GateFail},
		{"server dist artifact", "1\t1\tinternal/server/dist/bundle.js\n", GateFail},
		{"lockfile alone", "1\t5\tbun.lock\n", GateFail},
		{"lockfile with its manifest", "1\t5\tbun.lock\n1\t1\tpackage.json\n", GatePass},
		{"go.sum is never churn", "4\t9\tgo.sum\n", GatePass},
	}
	for _, tc := range cases {
		in := base()
		in.Numstat = tc.numstat
		g := mustGate(t, in, "no_churn")
		if g.State != tc.want {
			t.Errorf("%s: no_churn = %q (%s), want %s", tc.name, g.State, g.Detail, tc.want)
		}
	}
}

func TestDigest_FilesOutsideWriteScope(t *testing.T) {
	in := base()
	in.Numstat = "1\t1\tinternal/steward/a.go\n2\t2\tinternal/server/x.go\n"
	g := mustGate(t, in, "scope")
	if g.State != GateFail {
		t.Fatalf("scope = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "internal/server/x.go") {
		t.Errorf("detail %q does not list the outside path", g.Detail)
	}

	inside := base()
	inside.Numstat = "1\t1\tinternal/steward/a.go\n1\t1\tdocs/plans/active/steward-seat.md\n"
	if g := mustGate(t, inside, "scope"); g.State != GatePass {
		t.Errorf("scope = %q (%s), want pass for write scope + plan file", g.State, g.Detail)
	}
}

func TestDigest_StackedBranchDetected(t *testing.T) {
	in := base()
	in.Stacking.ForeignCommits = []string{"abc1234def", "56789abcde"}
	g := mustGate(t, in, "not_stacked")
	if g.State != GateFail {
		t.Fatalf("not_stacked = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "2 commit") {
		t.Errorf("detail %q does not count the foreign commits", g.Detail)
	}
}

func TestDigest_PRBaseNotDevelop(t *testing.T) {
	in := base()
	in.PR.Base = "main"
	g := mustGate(t, in, "not_stacked")
	if g.State != GateFail {
		t.Fatalf("not_stacked = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "PR base is main") {
		t.Errorf("detail %q does not name the base", g.Detail)
	}
}

func TestDigest_ConventionalCommitsTitle(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"feat(steward): digest gates", GatePass},
		{"fix: crash on empty numstat", GatePass},
		{"revert(deps)!: drop bundler", GatePass},
		{"Add digest gates", GateFail},
		{"feat digest gates", GateFail},
		{"unknown: not a known type", GateFail},
	}
	for _, tc := range cases {
		in := base()
		in.PR.Title = tc.title
		g := mustGate(t, in, "pr_hygiene")
		if g.State != tc.want {
			t.Errorf("title %q: pr_hygiene = %q (%s), want %s", tc.title, g.State, g.Detail, tc.want)
		}
	}
}

func TestDigest_PRBodyMissingNotDone(t *testing.T) {
	in := base()
	in.PR.Body = "## Validated\nall green\n"
	g := mustGate(t, in, "pr_hygiene")
	if g.State != GateFail {
		t.Fatalf("pr_hygiene = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "body") {
		t.Errorf("detail %q does not mention the body", g.Detail)
	}

	withNotVerified := base()
	withNotVerified.PR.Body = "## Validated\nx\n## Not verified\nnothing else\n"
	if g := mustGate(t, withNotVerified, "pr_hygiene"); g.State != GatePass {
		t.Errorf("pr_hygiene = %q (%s), want pass with a not-verified section", g.State, g.Detail)
	}
}

func TestDigest_CIStatesMapped(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"success", GatePass},
		{"failure", GateFail},
		{"pending", GateUnknown},
		{"none", GateUnknown},
		{"", GateUnknown},
		{"garbage", GateUnknown},
	}
	for _, tc := range cases {
		in := base()
		in.CI = CIInfo{State: tc.state}
		g := mustGate(t, in, "ci")
		if g.State != tc.want {
			t.Errorf("ci state %q: gate = %q (%s), want %s", tc.state, g.State, g.Detail, tc.want)
		}
	}

	none := base()
	none.CI = CIInfo{State: "none"}
	if g := mustGate(t, none, "ci"); !strings.Contains(g.Detail, "no CI result yet") {
		t.Errorf("detail %q, want the no-CI reason", g.Detail)
	}

	failing := base()
	failing.CI = CIInfo{State: "failure", Failing: []string{"unit", "lint"}}
	if g := mustGate(t, failing, "ci"); g.Detail != "unit, lint" {
		t.Errorf("detail %q, want the failing check names", g.Detail)
	}
}

func TestDigest_ValidatorStaleShaNotPass(t *testing.T) {
	in := base()
	in.Validation.CommitSHA = strings.Repeat("c", 40)
	g := mustGate(t, in, "validator")
	if g.State != GateFail {
		t.Fatalf("validator = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "stale") {
		t.Errorf("detail %q does not say stale", g.Detail)
	}

	nilValidation := base()
	nilValidation.Validation = nil
	if g := mustGate(t, nilValidation, "validator"); g.State != GateFail {
		t.Errorf("validator = %q, want fail with no validation", g.State)
	}
}

func TestDigest_FramesCapturedAtOtherShaFlagged(t *testing.T) {
	in := base()
	in.Numstat = "1\t1\tweb/packages/ui/src/x.tsx\n"
	in.Frames = framesAt(strings.Repeat("c", 40))
	g := mustGate(t, in, "frames_fresh")
	if g.State != GateFail {
		t.Fatalf("frames_fresh = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "captured at") {
		t.Errorf("detail %q does not name the capture sha", g.Detail)
	}

	noFrames := base()
	noFrames.Numstat = "1\t1\tweb/packages/ui/src/x.tsx\n"
	noFrames.Frames = nil
	if g := mustGate(t, noFrames, "frames_fresh"); g.State != GateFail || g.Detail != "no frames evidence" {
		t.Errorf("frames_fresh = %q (%s), want fail no frames evidence", g.State, g.Detail)
	}
}

func TestDigest_FramesMatrixMissingCell(t *testing.T) {
	in := base()
	in.Numstat = "1\t1\tweb/x.css\n"
	frames := framesAt(in.Task.HeadSHA)
	in.Frames = frames[:3] // drops {390,dark}, the last matrix cell
	g := mustGate(t, in, "frames_fresh")
	if g.State != GateFail {
		t.Fatalf("frames_fresh = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "390/dark") {
		t.Errorf("detail %q does not name the missing cell", g.Detail)
	}
}

func TestDigest_FramesNotRequiredForGoOnly(t *testing.T) {
	in := base()
	if g := mustGate(t, in, "frames_fresh"); g.State != GateNA {
		t.Fatalf("frames_fresh = %q, want n/a for a Go-only diff", g.State)
	}
}

func TestDigest_PeerHandoffNotVerifiedMustBeRepeated(t *testing.T) {
	notRepeated := base()
	notRepeated.Handoffs = []HandoffInfo{{Status: "done", NotVerified: []string{"did not run the race detector"}}}
	g := mustGate(t, notRepeated, "peer_claims")
	if g.State != GateFail {
		t.Fatalf("peer_claims = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "not repeated in the PR body") {
		t.Errorf("detail %q does not name the unclaimed item", g.Detail)
	}

	repeated := base()
	repeated.PR.Body = "## Validated\nx\n## Not done\ndid not run the race detector\n"
	repeated.Handoffs = notRepeated.Handoffs
	if g := mustGate(t, repeated, "peer_claims"); g.State != GatePass {
		t.Errorf("peer_claims = %q (%s), want pass when the body repeats the item", g.State, g.Detail)
	}

	failed := base()
	failed.Handoffs = []HandoffInfo{{Status: "failed"}}
	if g := mustGate(t, failed, "peer_claims"); g.State != GateFail {
		t.Errorf("peer_claims = %q, want fail for a failed handoff", g.State)
	}
}

func TestDigest_SecretScanCountsOnly(t *testing.T) {
	in := base()
	in.SecretHits = map[string]int{"token": 1, "aws-key": 1}
	in.PR.Body = "## Validated\nx\n## Not done\nsecret value AKIAIOSFODNN7EXAMPLE leaked\n"
	g := mustGate(t, in, "no_secrets")
	if g.State != GateFail {
		t.Fatalf("no_secrets = %q, want fail", g.State)
	}
	for _, want := range []string{"2 hit(s)", "aws-key=1", "token=1"} {
		if !strings.Contains(g.Detail, want) {
			t.Errorf("detail %q does not contain %q", g.Detail, want)
		}
	}
	if strings.Contains(g.Detail, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("detail %q contains a secret value", g.Detail)
	}
}

func TestGates_MutationsCommitted(t *testing.T) {
	stale := base()
	stale.Validation.CommitSHA = strings.Repeat("c", 40)
	g := mustGate(t, stale, "mutations_committed")
	if g.State != GateFail {
		t.Fatalf("mutations_committed = %q, want fail", g.State)
	}
	if !strings.Contains(g.Detail, "not head") {
		t.Errorf("detail %q does not say judged off head", g.Detail)
	}

	unrestored := base()
	unrestored.Validation.Mutations = []MutationInfo{{ID: "M1", Status: "pass", Restored: false}}
	g = mustGate(t, unrestored, "mutations_committed")
	if g.State != GateFail || !strings.Contains(g.Detail, "M1") {
		t.Fatalf("mutations_committed = %q (%s), want fail naming M1", g.State, g.Detail)
	}

	ok := base()
	ok.Validation.Mutations = []MutationInfo{{ID: "M1", Status: "pass", Restored: true}}
	if g := mustGate(t, ok, "mutations_committed"); g.State != GatePass {
		t.Errorf("mutations_committed = %q, want pass", g.State)
	}

	noContract := base()
	noContract.HasContract = false
	if g := mustGate(t, noContract, "mutations_committed"); g.State != GateNA {
		t.Errorf("mutations_committed = %q, want n/a without a contract", g.State)
	}
}

func TestParseNumstat_RenamesAndBlankLines(t *testing.T) {
	rows := parseNumstat("3\t1\tinternal/steward/a.go\n\n5\t2\told/path.go => new/path.go\n1\t0\tweb/{old => new}/x.tsx\n-\t-\tlogo.png\n\n")
	want := []numstatRow{
		{Added: 3, Deleted: 1, Path: "internal/steward/a.go"},
		{Added: 5, Deleted: 2, Path: "new/path.go"},
		{Added: 1, Deleted: 0, Path: "web/new/x.tsx"},
		{Binary: true, Path: "logo.png"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows %v, want %d", len(rows), rows, len(want))
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}
