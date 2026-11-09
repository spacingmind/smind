package taskrunner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
)

// acpForm builds an elicitation form request out of whole properties, the
// way decodeElicitation hands them to buildACPQuestionRequest.
func acpForm(message string, props ...acp.ElicitationProperty) acp.ElicitationRequest {
	return acp.ElicitationRequest{SessionID: "s1", Mode: "form", Message: message, Properties: props}
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func optionIDs(ids ...string) []string { return ids }

// TestBuildACPQuestionRequest_Table covers every property shape the ACP
// mapping models, one property per row: what kind, options and answer
// constraints each becomes.
func TestBuildACPQuestionRequest_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		prop  acp.ElicitationProperty
		check func(t *testing.T, q Question, f acpField)
	}{
		{
			"single enum with titles",
			acp.ElicitationProperty{Name: "env", Type: "string", Title: "Environment",
				Options: []acp.ElicitationOption{{Value: "prod", Title: "Production"}, {Value: "stg", Title: "Staging"}}},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionSingle || q.AllowOther {
					t.Fatalf("kind = %q allowOther = %v, want single without other", q.Kind, q.AllowOther)
				}
				if len(q.Options) != 2 || q.Options[0].ID != "o0" || q.Options[0].Label != "Production" || q.Options[1].Label != "Staging" {
					t.Fatalf("options = %+v", q.Options)
				}
				if f.kind != "enum" || !reflect.DeepEqual(f.values, []string{"prod", "stg"}) {
					t.Fatalf("field = %+v, want the original values kept for the answer", f)
				}
			},
		},
		{
			"plain enum without titles",
			acp.ElicitationProperty{Name: "zone", Type: "string", Options: []acp.ElicitationOption{{Value: "eu"}, {Value: "us"}}},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionSingle || q.Options[0].Label != "eu" || q.Options[1].Label != "us" {
					t.Fatalf("question = %+v, want the values as labels", q)
				}
			},
		},
		{
			"boolean",
			acp.ElicitationProperty{Name: "ok", Type: "boolean", Title: "Confirm"},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionSingle || len(q.Options) != 2 ||
					q.Options[0].Label != "Yes" || q.Options[1].Label != "No" || q.Options[1].ID != "o1" {
					t.Fatalf("question = %+v, want single Yes/No", q)
				}
				if !reflect.DeepEqual(f.values, []string{"true", "false"}) {
					t.Fatalf("field = %+v", f)
				}
			},
		},
		{
			"integer",
			acp.ElicitationProperty{Name: "count", Type: "integer", Minimum: floatPtr(1), Maximum: floatPtr(5)},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionText || len(q.Options) != 0 || q.valueHint != "integer" ||
					q.minVal == nil || *q.minVal != 1 || q.maxVal == nil || *q.maxVal != 5 {
					t.Fatalf("question = %+v field = %+v, want a constrained integer text field", q, f)
				}
			},
		},
		{
			"number",
			acp.ElicitationProperty{Name: "ratio", Type: "number", Minimum: floatPtr(0.5), Maximum: floatPtr(2.5)},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionText || q.valueHint != "number" {
					t.Fatalf("question = %+v, want a number text field", q)
				}
			},
		},
		{
			"plain string",
			acp.ElicitationProperty{Name: "nick", Type: "string", MinLength: intPtr(3), MaxLength: intPtr(8), Pattern: "^[a-z]+$"},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionText || q.minLen != 3 || q.maxLen != 8 || q.pattern == nil || q.pattern.String() != "^[a-z]+$" {
					t.Fatalf("question = %+v, want the length range and pattern carried", q)
				}
			},
		},
		{
			"array of enums is multi",
			acp.ElicitationProperty{Name: "tags", Type: "array", Options: []acp.ElicitationOption{{Value: "a"}, {Value: "b"}, {Value: "c"}}},
			func(t *testing.T, q Question, f acpField) {
				if q.Kind != QuestionMulti || len(q.Options) != 3 || f.kind != "multi" ||
					!reflect.DeepEqual(f.values, []string{"a", "b", "c"}) {
					t.Fatalf("question = %+v field = %+v, want a multi of the item choices", q, f)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			qr, fields := buildACPQuestionRequest(acpForm("", tc.prop))
			if qr.Refuse != "" || qr.Truncated {
				t.Fatalf("request = %+v, want it modelled", qr)
			}
			if qr.Source != QuestionSourceACP || len(qr.Questions) != 1 || len(fields) != 1 {
				t.Fatalf("request = %+v fields = %+v", qr, fields)
			}
			tc.check(t, qr.Questions[0], fields[0])
		})
	}

	// The form's message leads the first question's text only.
	qr, _ := buildACPQuestionRequest(acpForm("Pick a deploy target",
		acp.ElicitationProperty{Name: "a", Type: "string", Title: "A"},
		acp.ElicitationProperty{Name: "b", Type: "string", Title: "B"}))
	if qr.Questions[0].Text != "Pick a deploy target\nA" || qr.Questions[1].Text != "B" {
		t.Fatalf("texts = %q / %q, want the message leading only the first", qr.Questions[0].Text, qr.Questions[1].Text)
	}
}

// TestBuildACPQuestionRequest_RefusesWhole: a form the mapping cannot model
// comes back refused whole -- never partially answered.
func TestBuildACPQuestionRequest_RefusesWhole(t *testing.T) {
	t.Parallel()
	five := make([]acp.ElicitationProperty, 5)
	for i := range five {
		five[i] = acp.ElicitationProperty{Name: string(rune('a' + i)), Type: "string"}
	}
	for name, req := range map[string]acp.ElicitationRequest{
		"unsupported property": acpForm("", acp.ElicitationProperty{Name: "nested", Type: "object", Title: "Nested", Unsupported: true}),
		"zero properties":      {},
		"five properties":      acpForm("", five...),
	} {
		qr, fields := buildACPQuestionRequest(req)
		if qr.Refuse != QuestionResolvedUnsupported || len(qr.Questions) != 0 || fields != nil {
			t.Fatalf("%s: qr = %+v fields = %+v, want a whole refusal", name, qr, fields)
		}
	}
}

// TestBuildACPQuestionRequest_CapsAndStrips reuses the question model's cap
// and untrusted-text rules for agent-authored ACP text: a long title is cut
// with an ellipsis and flagged, options past the cap are dropped, and
// control/bidi/zero-width characters are stripped.
func TestBuildACPQuestionRequest_CapsAndStrips(t *testing.T) {
	t.Parallel()
	opts := make([]acp.ElicitationOption, 0, 13)
	opts = append(opts, acp.ElicitationOption{Value: "x", Title: "a\x1b[31mb\u200bc‮d"})
	for i := 0; i < 12; i++ {
		opts = append(opts, acp.ElicitationOption{Value: string(rune('a' + i))})
	}
	qr, fields := buildACPQuestionRequest(acpForm("", acp.ElicitationProperty{
		Name: "env", Type: "string", Title: strings.Repeat("T", 300), Options: opts,
	}))
	if !qr.Truncated {
		t.Fatal("a cut title and dropped options must flag Truncated")
	}
	q := qr.Questions[0]
	if got := []rune(q.Header); len(got) != maxQuestionHeader || got[len(got)-1] != '…' {
		t.Fatalf("header = %q (%d runes), want it capped at %d with an ellipsis", q.Header, len(got), maxQuestionHeader)
	}
	if got := []rune(q.Text); len(got) > maxQuestionText {
		t.Fatalf("text = %d runes, want at most %d", len(got), maxQuestionText)
	}
	if len(q.Options) != maxQuestionOptions {
		t.Fatalf("options = %d, want the cap %d (the rest dropped, not paged)", len(q.Options), maxQuestionOptions)
	}
	if q.Options[0].Label != "a[31mbcd" { // ESC, the zero-width space and the bidi override are gone; the CSI run's printable tail is text
		t.Fatalf("first option label = %q, want the stripped text", q.Options[0].Label)
	}
	want := append([]string{"x"}, []string{"a", "b", "c", "d", "e", "f", "g"}...)
	if !reflect.DeepEqual(fields[0].values, want) {
		t.Fatalf("values = %v, want the first %d originals", fields[0].values, maxQuestionOptions)
	}
}

// TestBuildACPQuestionRequest_SecretIsFlagged: every place a secret ask can
// hide in a form (a password format, a credential-looking property name, an
// option title, the overall message) marks the request.
func TestBuildACPQuestionRequest_SecretIsFlagged(t *testing.T) {
	t.Parallel()
	plain := acp.ElicitationProperty{Name: "env", Type: "string", Options: []acp.ElicitationOption{{Value: "prod"}, {Value: "stg"}}}
	for name, req := range map[string]acp.ElicitationRequest{
		"password format": acpForm("", acp.ElicitationProperty{Name: "cred", Type: "string", Format: "password"}),
		"property name":   acpForm("", acp.ElicitationProperty{Name: "api_key", Type: "string"}),
		"option title":    acpForm("", acp.ElicitationProperty{Name: "env", Type: "string", Options: []acp.ElicitationOption{{Value: "x", Title: "the password"}}}),
		"message":         acpForm("Enter the admin password", plain),
	} {
		qr, _ := buildACPQuestionRequest(req)
		if !AsksForSecret(qr) {
			t.Errorf("%s: not refused", name)
		}
	}
	qr, _ := buildACPQuestionRequest(acpForm("Pick a deploy target", plain))
	if AsksForSecret(qr) {
		t.Error("false positive on a plain form")
	}
}

// TestACPContent_TypesAndValues maps a validated human answer back to the
// elicitation content: the ORIGINAL enum values (not labels), a real bool,
// originals in the chosen order for a multi, real numbers, trimmed text.
func TestACPContent_TypesAndValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		prop     acp.ElicitationProperty
		item     QuestionAnswerItem
		expected any
	}{
		{
			"enum answers the original value",
			acp.ElicitationProperty{Name: "env", Type: "string", Options: []acp.ElicitationOption{{Value: "prod", Title: "Production"}, {Value: "stg", Title: "Staging"}}},
			QuestionAnswerItem{QuestionID: "q0", OptionIDs: optionIDs("o1")},
			"stg",
		},
		{
			"boolean true",
			acp.ElicitationProperty{Name: "ok", Type: "boolean"},
			QuestionAnswerItem{QuestionID: "q0", OptionIDs: optionIDs("o0")},
			true,
		},
		{
			"boolean false",
			acp.ElicitationProperty{Name: "ok", Type: "boolean"},
			QuestionAnswerItem{QuestionID: "q0", OptionIDs: optionIDs("o1")},
			false,
		},
		{
			"multi answers originals in the chosen order",
			acp.ElicitationProperty{Name: "tags", Type: "array", Options: []acp.ElicitationOption{{Value: "a"}, {Value: "b"}, {Value: "c"}}},
			QuestionAnswerItem{QuestionID: "q0", OptionIDs: optionIDs("o2", "o0")},
			[]string{"c", "a"},
		},
		{
			"integer",
			acp.ElicitationProperty{Name: "count", Type: "integer", Minimum: floatPtr(1), Maximum: floatPtr(5)},
			QuestionAnswerItem{QuestionID: "q0", Text: "3"},
			int64(3),
		},
		{
			"number",
			acp.ElicitationProperty{Name: "ratio", Type: "number"},
			QuestionAnswerItem{QuestionID: "q0", Text: "2.5"},
			2.5,
		},
		{
			"text is trimmed",
			acp.ElicitationProperty{Name: "note", Type: "string"},
			QuestionAnswerItem{QuestionID: "q0", Text: "  hi  "},
			"hi",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			qr, fields := buildACPQuestionRequest(acpForm("", tc.prop))
			if qr.Refuse != "" {
				t.Fatalf("request refused: %+v", qr)
			}
			ans := QuestionAnswer{Action: QuestionActionAnswer, Answers: []QuestionAnswerItem{tc.item}}
			if err := ValidateQuestionAnswer(qr, ans); err != nil {
				t.Fatalf("ValidateQuestionAnswer() error = %v", err)
			}
			content := acpContent(qr, fields, ans)
			if got := content[tc.prop.Name]; !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("content[%q] = %#v, want %#v", tc.prop.Name, got, tc.expected)
			}
		})
	}
}

// TestValidateQuestionAnswer_ACPConstraints: an ACP form field's answer
// constraints are checked before the agent sees the answer; a Claude-built
// question carries none.
func TestValidateQuestionAnswer_ACPConstraints(t *testing.T) {
	t.Parallel()
	build := func(p acp.ElicitationProperty) QuestionRequest {
		qr, _ := buildACPQuestionRequest(acpForm("", p))
		if qr.Refuse != "" {
			t.Fatalf("property %s refused: %+v", p.Name, qr)
		}
		return qr
	}
	text := func(qr QuestionRequest, s string) error {
		return ValidateQuestionAnswer(qr, QuestionAnswer{Action: QuestionActionAnswer,
			Answers: []QuestionAnswerItem{{QuestionID: qr.Questions[0].ID, Text: s}}})
	}

	integer := build(acp.ElicitationProperty{Name: "count", Type: "integer", Minimum: floatPtr(1), Maximum: floatPtr(5)})
	for _, bad := range []string{"abc", "2.5", "0", "6", ""} {
		if err := text(integer, bad); err == nil {
			t.Errorf("integer accepted %q", bad)
		}
	}
	if err := text(integer, "3"); err != nil {
		t.Errorf("integer rejected 3: %v", err)
	}

	number := build(acp.ElicitationProperty{Name: "ratio", Type: "number", Minimum: floatPtr(0.5), Maximum: floatPtr(2.5)})
	if err := text(number, "2.5"); err != nil {
		t.Errorf("number rejected 2.5: %v", err)
	}
	for _, bad := range []string{"abc", "0.4", "2.6"} {
		if err := text(number, bad); err == nil {
			t.Errorf("number accepted %q", bad)
		}
	}

	str := build(acp.ElicitationProperty{Name: "nick", Type: "string", MinLength: intPtr(2), MaxLength: intPtr(5)})
	if err := text(str, "abc"); err != nil {
		t.Errorf("string rejected abc: %v", err)
	}
	for _, bad := range []string{"a", "abcdef", "åååååå"} { // maxLength counts runes, not bytes
		if err := text(str, bad); err == nil {
			t.Errorf("string accepted %q", bad)
		}
	}
	if err := text(str, "åå"); err != nil { // 2 runes, 4 bytes: minLength too
		t.Errorf("string rejected a 2-rune answer: %v", err)
	}

	pattern := build(acp.ElicitationProperty{Name: "code", Type: "string", Pattern: "^[a-z]+$"})
	if err := text(pattern, "ABC"); err == nil {
		t.Error("pattern accepted ABC")
	}
	if err := text(pattern, "abc"); err != nil {
		t.Errorf("pattern rejected abc: %v", err)
	}

	// An RE2-uncompilable ECMA pattern is not enforced (there is no safe way
	// to run it), rather than blocking every answer. (?<x>a) is omitted
	// here: this Go accepts it as a named group, so it compiles fine.
	unc := build(acp.ElicitationProperty{Name: "weird", Type: "string", Pattern: `(?=a)`})
	if unc.Questions[0].pattern != nil {
		t.Fatal("an uncompilable pattern must not be kept")
	}
	if err := text(unc, "anything 2.5 !@#"); err != nil {
		t.Errorf("uncompilable pattern was enforced: %v", err)
	}

	// A Claude-built question has no constraints: any free text is valid.
	claude, err := ParseClaudeAskUserQuestion(loadAskInput(t, "single"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuestionAnswer(claude, QuestionAnswer{Action: QuestionActionAnswer,
		Answers: []QuestionAnswerItem{{QuestionID: "q0", Text: "2.5 not a number !@#"}}}); err != nil {
		t.Errorf("a Claude question enforced constraints: %v", err)
	}
}

// TestACPElicitationAdapter_Mapping adapts a QuestionDecider to the ACP
// elicitation policy: a human answer becomes accept with content, every
// no-answer outcome becomes decline, a cancelled call becomes cancel, a live
// error is returned, and a refused form reaches the decider with Refuse set.
func TestACPElicitationAdapter_Mapping(t *testing.T) {
	t.Parallel()
	form := acpForm("Pick a deploy target", acp.ElicitationProperty{Name: "env", Type: "string",
		Options: []acp.ElicitationOption{{Value: "prod", Title: "Production"}, {Value: "stg", Title: "Staging"}}})

	human := QuestionOutcome{Resolution: QuestionResolvedByHuman, Answer: QuestionAnswer{Action: QuestionActionAnswer,
		Answers: []QuestionAnswerItem{{QuestionID: "q0", OptionIDs: optionIDs("o1")}}}}

	t.Run("human answer accepts with content", func(t *testing.T) {
		t.Parallel()
		qd := &fakeQD{out: human}
		resp, err := acpElicitationAdapter{decider: qd}.Decide(context.Background(), form)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Action != acp.ElicitationAccept || !reflect.DeepEqual(resp.Content, map[string]any{"env": "stg"}) {
			t.Fatalf("response = %+v", resp)
		}
	})

	for _, res := range []QuestionResolution{QuestionResolvedDeclined, QuestionResolvedTimeout, QuestionResolvedProfile, QuestionResolvedSecret} {
		t.Run("decline outcome "+string(res), func(t *testing.T) {
			t.Parallel()
			qd := &fakeQD{out: QuestionOutcome{Resolution: res,
				Answer: QuestionAnswer{Action: QuestionActionDecline, Note: "no"}}}
			resp, err := acpElicitationAdapter{decider: qd}.Decide(context.Background(), form)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Action != acp.ElicitationDecline || resp.Content != nil {
				t.Fatalf("response = %+v, want a bare decline", resp)
			}
		})
	}

	t.Run("decider error under a cancelled ctx cancels", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		qd := &fakeQD{err: errors.New("run stopped")}
		resp, err := acpElicitationAdapter{decider: qd}.Decide(ctx, form)
		if err != nil || resp.Action != acp.ElicitationCancel {
			t.Fatalf("response = %+v err = %v, want cancel", resp, err)
		}
	})

	t.Run("decider error with a live ctx is returned", func(t *testing.T) {
		t.Parallel()
		qd := &fakeQD{err: errors.New("boom")}
		resp, err := acpElicitationAdapter{decider: qd}.Decide(context.Background(), form)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v, want the decider's error", err)
		}
		if resp.Action != "" || resp.Content != nil {
			t.Fatalf("response = %+v, want the zero value", resp)
		}
	})

	t.Run("refused form reaches the decider and declines", func(t *testing.T) {
		t.Parallel()
		bad := acpForm("", acp.ElicitationProperty{Name: "nested", Type: "object", Unsupported: true})
		qd := &fakeQD{out: QuestionOutcome{Resolution: QuestionResolvedUnsupported,
			Answer: QuestionAnswer{Action: QuestionActionDecline}}}
		resp, err := acpElicitationAdapter{decider: qd}.Decide(context.Background(), bad)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Action != acp.ElicitationDecline {
			t.Fatalf("response = %+v, want decline", resp)
		}
		if qd.got == nil || qd.got.Refuse != QuestionResolvedUnsupported {
			t.Fatalf("decider request = %+v, want Refuse set", qd.got)
		}
	})
}

// TestElicitationCtx_DefaultsToDisabled: without WithElicitation the flag is
// off, so a run that never said otherwise never advertises the capability.
func TestElicitationCtx_DefaultsToDisabled(t *testing.T) {
	t.Parallel()
	if ElicitationEnabled(context.Background()) {
		t.Fatal("a plain ctx must not enable elicitation")
	}
	ctx := WithElicitation(context.Background(), true)
	if !ElicitationEnabled(ctx) {
		t.Fatal("WithElicitation(true) was lost")
	}
	if ElicitationEnabled(WithElicitation(context.Background(), false)) {
		t.Fatal("WithElicitation(false) was lost")
	}
}

// --- end to end through Runner against the fake agent ---

// elicitingDecider answers the fake agent's deploy-target form the way a
// human would, and records what it was asked.
type elicitingDecider struct {
	decline bool
	got     *QuestionRequest
}

func (d *elicitingDecider) Decide(context.Context, string, []PermissionOption) (string, error) {
	return "allow-1", nil // the reply scenario raises no permission request
}

func (d *elicitingDecider) DecideQuestion(_ context.Context, req QuestionRequest) (QuestionOutcome, error) {
	got := req
	d.got = &got
	if d.decline {
		return QuestionOutcome{Resolution: QuestionResolvedDeclined,
			Answer: QuestionAnswer{Action: QuestionActionDecline, Note: "not now"}}, nil
	}
	items := make([]QuestionAnswerItem, 0, len(req.Questions))
	for _, q := range req.Questions {
		switch q.Header {
		case "Count":
			items = append(items, QuestionAnswerItem{QuestionID: q.ID, Text: "3"})
		case "Environment":
			items = append(items, QuestionAnswerItem{QuestionID: q.ID, OptionIDs: optionIDs("o1")})
		case "Note": // not in the fake agent's 4-property form; kept for the brief's answer table
			items = append(items, QuestionAnswerItem{QuestionID: q.ID, Text: "hi"})
		case "Confirm":
			items = append(items, QuestionAnswerItem{QuestionID: q.ID, OptionIDs: optionIDs("o0")})
		case "Tags":
			items = append(items, QuestionAnswerItem{QuestionID: q.ID, OptionIDs: optionIDs("o0", "o2")})
		default:
			return QuestionOutcome{}, errors.New("unexpected question header " + q.Header)
		}
	}
	return QuestionOutcome{Resolution: QuestionResolvedByHuman,
		Answer: QuestionAnswer{Action: QuestionActionAnswer, Answers: items}}, nil
}

// runACPElicitTurn runs one GLM turn against the fake agent's elicit=form
// scenario under ctx and returns the streamed text chunks.
func runACPElicitTurn(t *testing.T, ctx context.Context, d PermissionDecider) []string {
	t.Helper()
	r, _, task, _ := newMcpTestEnv(t, "", "elicit=form")
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderGLM, "hi", d, PermissionSettings{}, "", "", events)
	}()
	var texts []string
	for _, e := range drainEvents(events) {
		if e.Type == EventTypeText {
			texts = append(texts, e.Text)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return texts
}

func elicitText(t *testing.T, texts []string, prefix string) string {
	t.Helper()
	for _, s := range texts {
		if strings.HasPrefix(s, prefix) {
			return s
		}
	}
	t.Fatalf("no %q chunk in %v", prefix, texts)
	return ""
}

// TestRunACP_ElicitationEndToEnd: with the ctx flag on, Runner advertises
// the form capability, the agent's form reaches the QuestionDecider, and
// the human's answer goes back as the elicitation content.
func TestRunACP_ElicitationEndToEnd(t *testing.T) {
	d := &elicitingDecider{}
	texts := runACPElicitTurn(t, WithElicitation(context.Background(), true), d)
	if got := elicitText(t, texts, "caps:"); got != `caps:{"form":{}}` {
		t.Fatalf("caps chunk = %q, want the form capability only", got)
	}
	want := `elicitation:{"action":"accept","content":{"count":3,"env":"stg","ok":true,"tags":["a","c"]}}`
	if got := elicitText(t, texts, "elicitation:"); got != want {
		t.Fatalf("elicitation chunk = %q, want %q", got, want)
	}
	if d.got == nil || len(d.got.Questions) != 4 || d.got.Source != QuestionSourceACP {
		t.Fatalf("decider request = %+v, want the 4-field form", d.got)
	}
}

// TestRunACP_ElicitationNotAdvertisedWhenDisabled: without the ctx flag the
// capability is absent and the agent's request gets method-not-found -- the
// pre-ADR-0055 wire behaviour.
func TestRunACP_ElicitationNotAdvertisedWhenDisabled(t *testing.T) {
	d := &elicitingDecider{}
	texts := runACPElicitTurn(t, context.Background(), d)
	if got := elicitText(t, texts, "caps:"); got != "caps:none" {
		t.Fatalf("caps chunk = %q, want none", got)
	}
	if got := elicitText(t, texts, "elicitation:"); !strings.Contains(got, "method not found") {
		t.Fatalf("elicitation chunk = %q, want a method-not-found error", got)
	}
	if d.got != nil {
		t.Fatal("the decider was asked although the capability was off")
	}
}

// TestRunACP_ElicitationDeclineWhenHumanDeclines: a declined form goes back
// as ACP's bare decline.
func TestRunACP_ElicitationDeclineWhenHumanDeclines(t *testing.T) {
	d := &elicitingDecider{decline: true}
	texts := runACPElicitTurn(t, WithElicitation(context.Background(), true), d)
	if got := elicitText(t, texts, "elicitation:"); got != `elicitation:{"action":"decline"}` {
		t.Fatalf("elicitation chunk = %q, want a bare decline", got)
	}
}
