package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/version"
	"github.com/spacingmind/smind/internal/wsclient"
)

// ADR-0047 §1 and §6. A steward-seat run's built-in smind entry
// (SMIND_STEWARD_REVIEW_ID in its env) serves ONLY steward_digest_get and
// steward_verdict_submit: the digest to judge and the one verdict to
// submit, nothing of the Lead's catalogue. The one-time verdict token
// comes from the entry's env, never from a tool argument, so the model
// never sees it and cannot be talked into spending another review's
// verdict.

// stewardEnv is what a steward-seat run's smind entry carries.
type stewardEnv struct {
	ReviewID int64
	Token    string
}

// stewardEnvFromOS reads the steward seat env; ok is false for any other run.
func stewardEnvFromOS() (stewardEnv, bool, error) {
	raw := os.Getenv(steward.EnvReviewID)
	if raw == "" {
		return stewardEnv{}, false, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	tok := os.Getenv(steward.EnvToken)
	if err != nil || id <= 0 || tok == "" {
		return stewardEnv{}, true, errors.New("invalid " + steward.EnvReviewID + " / " + steward.EnvToken)
	}
	return stewardEnv{ReviewID: id, Token: tok}, true, nil
}

// newStewardMCPServer is the steward seat's whole catalogue: exactly two
// tools, never the annotator.
func newStewardMCPServer(client *wsclient.Client, env stewardEnv) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "smind", Title: "sMind steward", Version: version.Version}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: steward.ToolDigestGet, Description: "The review digest you are judging: objective gates computed by the daemon, diff facts, frames, and the untrusted text block. Authoritative; read it first."}, mcpStewardDigestGet(client, env))
	mcp.AddTool(srv, &mcp.Tool{Name: steward.ToolVerdictSubmit, Description: "Submit your one verdict (ACCEPTED, CHANGES or BLOCKED) with gate acknowledgements and a per-frame observation. Pass the whole verdict document as the `submission` argument; its own `verdict` field holds the ACCEPTED|CHANGES|BLOCKED string. The daemon rejects an incomplete verdict and names the missing item: fix it and submit again. Accepted once.", InputSchema: stewardVerdictSchema()}, mcpStewardVerdictSubmit(client, env))
	return srv
}

func mcpStewardDigestGet(client *wsclient.Client, env stewardEnv) mcp.ToolHandlerFor[struct{}, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		var out map[string]any
		if err := callWS(ctx, client, steward.ToolDigestGet, "steward.digest.get", map[string]any{"reviewId": env.ReviewID}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

type stewardVerdictSubmitInput struct {
	Submission steward.SeatVerdict `json:"submission" jsonschema:"the verdict document, an object: {v:1, reviewId, verdict: the string ACCEPTED|CHANGES|BLOCKED, summary, findings:[{file,line,why,check,severity}], gates:[{id,ack,note}], frames:[{path,observation}], blockedReason (BLOCKED only)}"`
}

// stewardVerdictSchema is the tool's input schema with the daemon's field
// caps in it, so the model sees them before it submits. The daemon counts
// bytes; JSON Schema maxLength counts characters, so the descriptions say
// bytes and the daemon's error text still names the cap.
func stewardVerdictSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[stewardVerdictSubmitInput](nil)
	if err != nil {
		panic("steward verdict schema: " + err.Error())
	}
	root := schema.Properties["submission"]
	for path, n := range steward.SchemaCaps() {
		node := root
		parts := strings.Split(path, ".")
		for _, part := range parts {
			name := strings.TrimSuffix(part, "[]")
			node = node.Properties[name]
			if strings.HasSuffix(part, "[]") {
				node = node.Items
			}
		}
		max := n
		node.MaxLength = &max
		node.Description = strings.TrimSpace(node.Description + fmt.Sprintf(" At most %d bytes (UTF-8).", n))
	}
	for name, n := range steward.ArrayCaps() {
		max := n
		root.Properties[name].MaxItems = &max
	}
	return schema
}

func mcpStewardVerdictSubmit(client *wsclient.Client, env stewardEnv) mcp.ToolHandlerFor[stewardVerdictSubmitInput, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in stewardVerdictSubmitInput) (*mcp.CallToolResult, map[string]any, error) {
		raw, err := json.Marshal(in.Submission)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: submission: %w", steward.ToolVerdictSubmit, err)
		}
		var out map[string]any
		params := map[string]any{"reviewId": env.ReviewID, "token": env.Token, "submission": json.RawMessage(raw)}
		if err := callWS(ctx, client, steward.ToolVerdictSubmit, "steward.verdict.submit", params, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}
