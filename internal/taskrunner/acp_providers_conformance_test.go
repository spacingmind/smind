package taskrunner

import (
	"context"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

// acpConformanceIDs are the five built-in ACP providers registered purely
// as data in internal/providers (opencode, copilot, cline, goose, cursor):
// none of them has any vendor-specific code in this package, so each must
// pass the same fake-agent conformance scenarios through the shared runACP
// path. These tests override the provider's command through the real
// WithACPCommand seam (not the test-only newACPClient override), which is
// the one substitution mechanism RunPrompt's ACP path actually exposes.
var acpConformanceIDs = []string{
	providers.IDOpenCode,
	providers.IDCopilot,
	providers.IDCline,
	providers.IDGoose,
	providers.IDCursor,
}

// otherACPCommandOpts points every ACP provider except skip at an
// unspawnable path. ProviderCatalog under WithACPModeProbe kicks a
// background probe for every ACP provider whose catalog is unknown, and
// several of these CLIs (opencode, cursor-agent, npx for GLM) are real
// binaries on a dev host -- without this, one catalog read would spawn
// them all. Mirrors acp_modes_test.go's WithACPCommand(ProviderKimi,
// "/nonexistent/kimi") guard.
func otherACPCommandOpts(skip Provider) []Option {
	var opts []Option
	for _, id := range append([]string{providers.IDGLM, providers.IDKimi}, acpConformanceIDs...) {
		if p := Provider(id); p != skip {
			opts = append(opts, WithACPCommand(p, []string{"/nonexistent/agent"}))
		}
	}
	return opts
}

// TestACPDiscovery_ConfigOptionsModelsAndModes proves mode/model discovery
// for the five data-registered ACP providers is entirely generic: each
// provider's probe reads the same category-"mode" select option (modes) and
// category-"model" select option (models) out of the fake agent's
// session/new configOptions, with no per-vendor code anywhere in the loop.
func TestACPDiscovery_ConfigOptionsModelsAndModes(t *testing.T) {
	for _, id := range acpConformanceIDs {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			p := Provider(id)
			opts := append([]Option{
				WithACPModeProbe(),
				WithACPCommand(p, []string{fakeACPAgentPath, "modes:config", "models"}),
			}, otherACPCommandOpts(p)...)
			r := New(nil, opts...)

			info := waitForDiscoveredModes(t, r, p)
			if !info.ModesDiscovered {
				t.Fatalf("%s ModesDiscovered = false", p)
			}
			if got := strings.Join(modeIDs(info.Modes), ","); got != "default,accept_edits,bypass_permissions" {
				t.Fatalf("%s modes = %s, want default,accept_edits,bypass_permissions", p, got)
			}
			if info.DefaultMode != "default" {
				t.Fatalf("%s default mode = %q, want %q", p, info.DefaultMode, "default")
			}

			if !info.ModelsDiscovered {
				t.Fatalf("%s ModelsDiscovered = false (models: %+v)", p, info.Models)
			}
			wantModels := []string{"fake-model-a", "fake-model-b"}
			if len(info.Models) != len(wantModels) {
				t.Fatalf("%s models = %+v, want exactly %v", p, info.Models, wantModels)
			}
			for i, want := range wantModels {
				m := info.Models[i]
				if m.ID != want || m.Source != ModelSourceDiscovered {
					t.Fatalf("%s models[%d] = %+v, want id %q with source %q", p, i, m, want, ModelSourceDiscovered)
				}
				// The agent reports fake-model-a as its current value, so
				// discovery must mark it the default.
				if m.Default != (want == "fake-model-a") {
					t.Fatalf("%s models[%d].Default = %v, want %v", p, i, m.Default, want == "fake-model-a")
				}
			}
		})
	}
}

// runFakeAgentPrompt drives one RunPrompt turn against the fake ACP agent
// for provider p (whose command the runner already points at it) and
// returns the drained events, failing the test if the run errors.
func runFakeAgentPrompt(t *testing.T, r *Runner, taskID int64, p Provider, prompt string, decider PermissionDecider) []Event {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), taskID, taskID, p, prompt, decider, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt(%s) error = %v", p, err)
	}
	return got
}

// textEvents returns just the run's text chunks, concatenated.
func textEvents(events []Event) []string {
	var texts []string
	for _, e := range events {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	return texts
}

// TestACPProvider_AllFive_FakeConformance drives each of the five
// data-registered ACP providers through the same three fake-agent
// scenarios, mirroring the existing GLM tests one for one (no new
// fake-agent features):
//
//   - permission: TestRunner_RunPrompt_PermissionRequest_GLM -- a real
//     session/request_permission round trip reaches the PermissionDecider
//     once, and the chosen optionId comes back in the agent's reply.
//   - resume: TestRunner_RunPrompt_GLM_ResumeCapabilityMatrix's
//     "loadSession" case -- the second run resumes the first's session
//     via session/load.
//   - cancel: TestRunner_RunPrompt_ContextCancellationStopsSubprocess /
//     cancelRunAfterFirstChunk -- the fake agent's "hang" scenario blocks
//     forever, and cancelling RunPrompt's context stops the turn.
func TestACPProvider_AllFive_FakeConformance(t *testing.T) {
	for _, id := range acpConformanceIDs {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			p := Provider(id)

			t.Run("permission", func(t *testing.T) {
				t.Parallel()
				wm, task := newTestTask(t, "permission")
				r := New(wm, WithACPCommand(p, []string{fakeACPAgentPath}))
				decider := &stubDecider{optionID: "allow-1"}

				got := runFakeAgentPrompt(t, r, task.ID, p, "hi", decider)

				if decider.callCount() != 1 {
					t.Fatalf("%s decider called %d times, want 1", p, decider.callCount())
				}
				if decider.summary != "Run a risky command" {
					t.Fatalf("%s decider.summary = %q, want %q", p, decider.summary, "Run a risky command")
				}
				if len(decider.options) != 2 || decider.options[0] != (PermissionOption{ID: "allow-1", Label: "Allow", Kind: "allow_once"}) {
					t.Fatalf("%s decider.options = %+v, want the agent's allow/deny pair", p, decider.options)
				}
				if texts := textEvents(got); len(texts) != 1 || texts[0] != "chose:allow-1" {
					t.Fatalf("%s texts = %v, want [%q]", p, texts, "chose:allow-1")
				}
				if last := got[len(got)-1]; last.Type != EventTypeDone {
					t.Fatalf("%s last event = %+v, want EventTypeDone", p, last)
				}
			})

			t.Run("resume", func(t *testing.T) {
				t.Parallel()
				wm, task := newTestTask(t, "")
				r := New(wm, WithACPCommand(p, []string{fakeACPAgentPath, "loadSession"}))

				runFakeAgentPrompt(t, r, task.ID, p, "hi", nil)
				if m := sessionInitMethod(t, *task.WorktreePath); m != "session/new" {
					t.Fatalf("%s first run session-init-method = %q, want %q", p, m, "session/new")
				}

				runFakeAgentPrompt(t, r, task.ID, p, "what did I say before?", nil)
				if m := sessionInitMethod(t, *task.WorktreePath); m != "session/load" {
					t.Fatalf("%s second run session-init-method = %q, want %q (resumed the first run's session)", p, m, "session/load")
				}
			})

			t.Run("cancel", func(t *testing.T) {
				t.Parallel()
				wm, task := newTestTask(t, "hang")
				r := New(wm, WithACPCommand(p, []string{fakeACPAgentPath}))

				cancelRunAfterFirstChunk(t, r, task, p)
			})
		})
	}
}
