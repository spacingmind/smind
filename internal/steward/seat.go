package steward

import "strconv"

// SeatProfileName is the steward seat's role-pack profile (ADR-0047 §1).
const SeatProfileName = "steward"

// The env vars of the seat's MCP entry: the review id and the one-time
// verdict token. The token appears nowhere else.
const (
	EnvReviewID = "SMIND_STEWARD_REVIEW_ID"
	EnvToken    = "SMIND_STEWARD_TOKEN"
)

// SeatBuiltinTools are the ONLY provider tools the seat may use.
var SeatBuiltinTools = []string{"Read", "Grep", "Glob"}

// SeatDisallowedTools are denied at the provider (claude --disallowedTools):
// no shell, no write, no web, no sub-agent.
var SeatDisallowedTools = []string{
	"Bash", "BashOutput", "KillShell",
	"Edit", "MultiEdit", "Write", "NotebookEdit",
	"WebFetch", "WebSearch",
	"Task", "SlashCommand", "TodoWrite",
}

// The seat's MCP tool names (ADR-0047 §1 and §6).
const (
	ToolDigestGet     = "steward_digest_get"
	ToolVerdictSubmit = "steward_verdict_submit"
	ToolAnnotate      = "steward_annotate"
)

// SeatMCPTools is the seat's MCP catalogue: two tools, plus
// steward_annotate only when annotate is on.
func SeatMCPTools(annotate bool) []string {
	tools := []string{ToolDigestGet, ToolVerdictSubmit}
	if annotate {
		tools = append(tools, ToolAnnotate)
	}
	return tools
}

// SeatAllowedTools is SeatBuiltinTools followed by SeatMCPTools(annotate).
func SeatAllowedTools(annotate bool) []string {
	allowed := make([]string, 0, len(SeatBuiltinTools)+3)
	allowed = append(allowed, SeatBuiltinTools...)
	allowed = append(allowed, SeatMCPTools(annotate)...)
	return allowed
}

// SeatMCPEnv builds the seat's MCP entry env additions: ONLY the review id
// and the one-time token (the token never goes anywhere else).
func SeatMCPEnv(reviewID int64, token string) map[string]string {
	return map[string]string{
		EnvReviewID: strconv.FormatInt(reviewID, 10),
		EnvToken:    token,
	}
}
