package gate

// Action is what an intent wants to do to the skill library.
type Action string

const (
	ActionCreate Action = "create"
	ActionPatch  Action = "patch"
	ActionDelete Action = "delete"
)

// Evidence is one provenance item cited by an intent; Quote must occur
// verbatim in the referenced window event.
type Evidence struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Seq   int64  `json:"seq"`
	Quote string `json:"quote"`
}

// Intent is the model's proposal. The gate validates it; nothing else turns
// it into a skill.
type Intent struct {
	Action       Action     `json:"action"`
	Name         string     `json:"name"`
	Category     string     `json:"category"`
	Description  string     `json:"description"`
	Body         string     `json:"body"`
	OldString    string     `json:"old_string"`
	NewString    string     `json:"new_string"`
	AbsorbedInto string     `json:"absorbed_into"`
	Reason       string     `json:"reason"`
	Evidence     []Evidence `json:"evidence"`
}

// Evidence kinds that can justify a skill.
const (
	EvUserMessage      = "user_message"
	EvFailureRecovery  = "failure_recovery"
	EvPermissionDenied = "permission_denied"
	EvLoadedSkillWrong = "loaded_skill_wrong"
)

// EventKey identifies one event in the reflected window.
type EventKey struct {
	RunID string
	Seq   int64
}

// WindowEvent is one window event; Kind is one of the Ev* kinds for
// qualifying events, or anything else (e.g. "tool_output", "assistant") for
// non-qualifying ones.
//
// Collector contract (the gate cannot verify it; the code that builds the
// Window MUST honour it, because Text is what grounds quotes and code spans):
//   - user_message: text typed by a human in the composer or queue with
//     source=human. NEVER tool results, hook or skill-index injections,
//     slash-command expansions or pasted tool output.
//   - failure_recovery: the failing tool call's one-line summary plus the
//     AGENT's recovery action, but NOT the repo-controlled failing tool output.
//   - permission_denied: the denied action's summary.
//   - loaded_skill_wrong: the loaded skill's name plus the contradicting
//     human or agent text.
type WindowEvent struct {
	Kind string
	Text string
}

// Window maps event keys to events. A Window must be built for ONE workspace
// only: events from several workspaces would let evidence from one ground a
// skill proposed for another.
type Window map[EventKey]WindowEvent

// Mode is which job produced the intent.
type Mode string

const (
	ModeReflect Mode = "reflect"
	ModeCurate  Mode = "curate"
)

// LiveSkill is one entry of the library snapshot passed to the gate.
// Status: "active"/"disabled" (stored learned skills), "proposed" (a pending
// proposal; compared for duplicates/collisions but never a patch/absorb
// target). Human skills have Human=true, Body always "" (the gate never
// receives human bodies), Status "".
type LiveSkill struct {
	Name        string
	Category    string
	Description string
	Body        string
	Status      string
	Human       bool
}

// Skill is a gate result ready for rendering.
type Skill struct {
	Name        string
	Category    string
	Description string
	Body        string
}

// Config carries the gate's tunables.
type Config struct {
	Limits    Limits
	Duplicate float64
	Possible  float64
	// Redact, if non-nil, is the injected secret scrubber; a value that
	// changes under Redact fails the gate.
	Redact func(string) string
	Mode   Mode
}

// Verdict is the gate's decision.
type Verdict struct {
	OK                  bool
	Findings            []Finding
	Result              Skill
	DuplicateOf         string
	PossibleDuplicateOf string
	Similarity          float64
}
