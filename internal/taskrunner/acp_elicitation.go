package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/acp"
)

// ADR-0055 section 4 (ACP): an agent's `elicitation/create` form becomes the
// provider-agnostic QuestionRequest a human answers. The mapping is closed: any
// property or form it cannot model is refused whole (declined, nothing shown),
// never partly answered, and the same secret, cap and untrusted-text rules as
// every other question apply (the builder reuses the Claude path's helpers).

// QuestionSourceACP marks a request that came from an ACP elicitation.
const QuestionSourceACP = "acp.elicitation"

type elicitationCtxKey struct{}

// WithElicitation returns ctx saying whether an ACP run may advertise the
// elicitation capability (ADR-0055 D4: only a run whose profile sets
// `questions: ask`). The registry decides; the runner reads it with
// ElicitationEnabled.
func WithElicitation(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, elicitationCtxKey{}, enabled)
}

// ElicitationEnabled reports WithElicitation's value (false when never set).
func ElicitationEnabled(ctx context.Context) bool {
	v, _ := ctx.Value(elicitationCtxKey{}).(bool)
	return v
}

// acpField remembers, per form property, how its answer maps back to the
// elicitation content: kind, and for a choice the option values in the order
// the question presents them (option id "o<i>" is index i).
type acpField struct {
	name   string
	kind   string // "enum", "multi", "string", "number", "integer", "boolean"
	values []string
}

// buildACPQuestionRequest maps an ACP form to a QuestionRequest and the field
// table needed to map the answer back. A form it cannot model comes back with
// Refuse set (the decider records it and declines).
func buildACPQuestionRequest(req acp.ElicitationRequest) (QuestionRequest, []acpField) {
	qr := QuestionRequest{Source: QuestionSourceACP}
	refuse := func(why QuestionResolution) (QuestionRequest, []acpField) {
		qr.Refuse = why
		return qr, nil
	}
	if len(req.Properties) == 0 || len(req.Properties) > maxQuestions {
		return refuse(QuestionResolvedUnsupported)
	}
	// The form's overall message leads the first question; a secret word in the
	// message counts for the whole request.
	msg, cut := capQuestionText(req.Message, maxQuestionText)
	qr.Truncated = qr.Truncated || cut
	qr.secret = qr.secret || mentionsSecret(req.Message)

	var fields []acpField
	for i, p := range req.Properties {
		if p.Unsupported {
			return refuse(QuestionResolvedUnsupported)
		}
		if strings.EqualFold(p.Format, "password") {
			qr.secret = true
		}
		qr.secret = qr.secret || mentionsSecret(p.Name) || mentionsSecret(p.Title) || mentionsSecret(p.Description)
		q := Question{ID: fmt.Sprintf("q%d", i)}
		f := acpField{name: p.Name}
		label := p.Title
		if label == "" {
			label = p.Name
		}
		text := label
		if p.Description != "" {
			text = label + "\n" + p.Description
		}
		if i == 0 && strings.TrimSpace(msg) != "" {
			text = msg + "\n" + text
		}
		var cutQ bool
		q.Text, cutQ = capQuestionText(text, maxQuestionText)
		qr.Truncated = qr.Truncated || cutQ
		q.Header, cutQ = capQuestionText(label, maxQuestionHeader)
		qr.Truncated = qr.Truncated || cutQ

		switch p.Type {
		case "string":
			if len(p.Options) > 0 {
				q.Kind, f.kind = QuestionSingle, "enum"
				qr.Truncated = addACPOptions(&q, &f, p.Options, &qr) || qr.Truncated
			} else {
				q.Kind, f.kind = QuestionText, "string"
				q.minLen, q.maxLen = derefInt(p.MinLength), derefInt(p.MaxLength)
				q.pattern = compileACPPattern(p.Pattern)
			}
		case "number", "integer":
			q.Kind, f.kind = QuestionText, p.Type
			q.valueHint = p.Type
			q.minVal, q.maxVal = p.Minimum, p.Maximum
		case "boolean":
			q.Kind, f.kind = QuestionSingle, "boolean"
			f.values = []string{"true", "false"}
			q.Options = []QuestionOption{{ID: "o0", Label: "Yes", orig: "Yes"}, {ID: "o1", Label: "No", orig: "No"}}
		case "array":
			q.Kind, f.kind = QuestionMulti, "multi"
			qr.Truncated = addACPOptions(&q, &f, p.Options, &qr) || qr.Truncated
		default:
			return refuse(QuestionResolvedUnsupported)
		}
		qr.Questions = append(qr.Questions, q)
		fields = append(fields, f)
	}
	return qr, fields
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// compileACPPattern compiles an ECMA-262 pattern as RE2; one RE2 cannot read is
// not enforced here (the agent validates its own pattern).
func compileACPPattern(p string) *regexp.Regexp {
	if p == "" {
		return nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil
	}
	return re
}

// addACPOptions copies a property's choices (capped) into the question, keeping
// the original values for the answer. It reports whether it dropped anything.
func addACPOptions(q *Question, f *acpField, opts []acp.ElicitationOption, qr *QuestionRequest) bool {
	dropped := false
	for j, o := range opts {
		if j >= maxQuestionOptions {
			dropped = true
			break
		}
		title := o.Title
		if title == "" {
			title = o.Value
		}
		qr.secret = qr.secret || mentionsSecret(title)
		label, cut := capQuestionText(title, maxOptionLabel)
		dropped = dropped || cut
		q.Options = append(q.Options, QuestionOption{ID: fmt.Sprintf("o%d", j), Label: label, orig: title})
		f.values = append(f.values, o.Value)
	}
	return dropped
}

// acpContent maps a validated human answer back to the elicitation content.
func acpContent(req QuestionRequest, fields []acpField, a QuestionAnswer) map[string]any {
	byQ := make(map[string]QuestionAnswerItem, len(a.Answers))
	for _, it := range a.Answers {
		byQ[it.QuestionID] = it
	}
	content := make(map[string]any, len(fields))
	for i, f := range fields {
		it := byQ[req.Questions[i].ID]
		text := strings.TrimSpace(it.Text)
		switch f.kind {
		case "enum":
			if len(it.OptionIDs) == 1 {
				content[f.name] = f.values[optionIndex(it.OptionIDs[0])]
			} else {
				content[f.name] = text // a free-text "other" is not offered for enums; defensive
			}
		case "boolean":
			content[f.name] = len(it.OptionIDs) == 1 && it.OptionIDs[0] == "o0"
		case "multi":
			vals := make([]string, 0, len(it.OptionIDs))
			for _, id := range it.OptionIDs {
				vals = append(vals, f.values[optionIndex(id)])
			}
			content[f.name] = vals
		case "integer":
			n, _ := strconv.ParseInt(text, 10, 64)
			content[f.name] = n
		case "number":
			n, _ := strconv.ParseFloat(text, 64)
			content[f.name] = n
		default:
			content[f.name] = text
		}
	}
	return content
}

func optionIndex(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "o"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// acpElicitationAdapter adapts a QuestionDecider to acp.ElicitationPolicy.
type acpElicitationAdapter struct {
	decider QuestionDecider
}

func (a acpElicitationAdapter) Decide(ctx context.Context, req acp.ElicitationRequest) (acp.ElicitationResponse, error) {
	qr, fields := buildACPQuestionRequest(req)
	out, err := a.decider.DecideQuestion(ctx, qr)
	if err != nil {
		// A stopped run (the decider returns the run's context error while the
		// per-call context here is a plain background one) or a cancelled call is
		// ACP's own "cancel", never an RPC error the agent would read as a bug.
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return acp.ElicitationResponse{Action: acp.ElicitationCancel}, nil
		}
		return acp.ElicitationResponse{}, err
	}
	if out.Answer.Action != QuestionActionAnswer || fields == nil {
		// A decline, a timeout, a refusal: ACP's decline carries no note (the
		// decider already recorded why in the run's history).
		return acp.ElicitationResponse{Action: acp.ElicitationDecline}, nil
	}
	return acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: acpContent(qr, fields, out.Answer)}, nil
}
