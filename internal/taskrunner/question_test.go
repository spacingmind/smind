package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func loadAskInput(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/askuserquestion/" + name + ".input.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseClaudeAskUserQuestion_Fixtures(t *testing.T) {
	req, err := ParseClaudeAskUserQuestion(loadAskInput(t, "single"))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Questions) != 1 || req.Questions[0].Kind != QuestionSingle || len(req.Questions[0].Options) != 2 {
		t.Fatalf("single: %+v", req)
	}
	if o := req.Questions[0].Options[1]; o.ID != "o1" || o.Label != "Blue" || o.Description == "" {
		t.Fatalf("option: %+v", o)
	}
	req, err = ParseClaudeAskUserQuestion(loadAskInput(t, "multi"))
	if err != nil || req.Questions[0].Kind != QuestionMulti || len(req.Questions[0].Options) != 3 {
		t.Fatalf("multi: %+v %v", req, err)
	}
}

// The updatedInput shape is pinned by the recorded CLI run (testdata README).
func TestClaudeAnswersInput_GoldenAgainstRecordedShape(t *testing.T) {
	cases := []struct {
		fixture string
		item    QuestionAnswerItem
		want    string
	}{
		{"single", QuestionAnswerItem{QuestionID: "q0", OptionIDs: []string{"o1"}}, "Blue"},
		{"single", QuestionAnswerItem{QuestionID: "q0", Text: "Green"}, "Green"},
		{"multi", QuestionAnswerItem{QuestionID: "q0", OptionIDs: []string{"o0", "o1"}}, "Red, Blue"},
	}
	for _, c := range cases {
		in := loadAskInput(t, c.fixture)
		req, _ := ParseClaudeAskUserQuestion(in)
		got := ClaudeAnswersInput(in, req, QuestionAnswer{Action: QuestionActionAnswer, Answers: []QuestionAnswerItem{c.item}})
		qtext := in["questions"].([]any)[0].(map[string]any)["question"].(string)
		want := map[string]any{}
		for k, v := range in {
			want[k] = v
		}
		want["answers"] = map[string]any{qtext: c.want}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v want %v", c.fixture, got, want)
		}
		if _, mutated := in["answers"]; mutated {
			t.Fatal("original input was modified")
		}
	}
}

func TestParseClaudeAskUserQuestion_CapsAndFlags(t *testing.T) {
	long := strings.Repeat("x", 2000)
	opts := []any{}
	for i := 0; i < 12; i++ {
		opts = append(opts, map[string]any{"label": "L", "description": long})
	}
	req, err := ParseClaudeAskUserQuestion(map[string]any{"questions": []any{
		map[string]any{"question": long, "header": long, "options": opts},
	}})
	if err != nil {
		t.Fatal(err)
	}
	q := req.Questions[0]
	if !req.Truncated || len([]rune(q.Text)) > maxQuestionText || len([]rune(q.Header)) > maxQuestionHeader ||
		len(q.Options) != maxQuestionOptions || len([]rune(q.Options[0].Description)) > maxOptionDesc {
		t.Fatalf("caps not applied: truncated=%v text=%d header=%d opts=%d", req.Truncated, len([]rune(q.Text)), len([]rune(q.Header)), len(q.Options))
	}
}

func TestParseClaudeAskUserQuestion_StripsControlCharacters(t *testing.T) {
	req, err := ParseClaudeAskUserQuestion(map[string]any{"questions": []any{
		map[string]any{"question": "ok\x1b[31m red\x00?", "options": []any{map[string]any{"label": "a\x07b"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(req.Questions[0].Text+req.Questions[0].Options[0].Label, "\x1b\x00\x07") {
		t.Fatalf("control characters survived: %q", req.Questions[0].Text)
	}
}

func TestParseClaudeAskUserQuestion_RefusesMalformed(t *testing.T) {
	bad := []map[string]any{
		{},
		{"questions": []any{}},
		{"questions": []any{map[string]any{"question": "q"}}},
		{"questions": []any{map[string]any{"question": "", "options": []any{map[string]any{"label": "a"}}}}},
		{"questions": []any{1}},
	}
	five := []any{}
	for i := 0; i < 5; i++ {
		five = append(five, map[string]any{"question": "q", "options": []any{map[string]any{"label": "a"}}})
	}
	bad = append(bad, map[string]any{"questions": five})
	for i, in := range bad {
		if _, err := ParseClaudeAskUserQuestion(in); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestAsksForSecret(t *testing.T) {
	mk := func(header, text, label string) QuestionRequest {
		return QuestionRequest{Questions: []Question{{Header: header, Text: text, Options: []QuestionOption{{Label: label}}}}}
	}
	for _, r := range []QuestionRequest{
		mk("", "What is the API key for the service?", "x"),
		mk("Auth", "Paste your password", "x"),
		mk("", "ok?", "use my token"),
		mk("Credentials", "?", "x"),
	} {
		if !AsksForSecret(r) {
			t.Fatalf("not refused: %+v", r)
		}
	}
	if AsksForSecret(mk("Colour", "Which colour do you prefer?", "Red")) {
		t.Fatal("false positive on a plain question")
	}
}

func TestValidateQuestionAnswer(t *testing.T) {
	single := QuestionRequest{Questions: []Question{{ID: "q0", Text: "t", Kind: QuestionSingle, AllowOther: true, Options: []QuestionOption{{ID: "o0", Label: "A"}, {ID: "o1", Label: "B"}}}}}
	ok := func(a QuestionAnswer) error { return ValidateQuestionAnswer(single, a) }
	ans := func(it QuestionAnswerItem) QuestionAnswer {
		return QuestionAnswer{Action: QuestionActionAnswer, Answers: []QuestionAnswerItem{it}}
	}
	if err := ok(ans(QuestionAnswerItem{QuestionID: "q0", OptionIDs: []string{"o1"}})); err != nil {
		t.Fatal(err)
	}
	if err := ok(ans(QuestionAnswerItem{QuestionID: "q0", Text: "other"})); err != nil {
		t.Fatal(err)
	}
	if err := ok(QuestionAnswer{Action: QuestionActionDecline, Note: "no"}); err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]QuestionAnswer{
		"unknown option":   ans(QuestionAnswerItem{QuestionID: "q0", OptionIDs: []string{"o9"}}),
		"two choices":      ans(QuestionAnswerItem{QuestionID: "q0", OptionIDs: []string{"o0", "o1"}}),
		"choice and text":  ans(QuestionAnswerItem{QuestionID: "q0", OptionIDs: []string{"o0"}, Text: "x"}),
		"nothing":          ans(QuestionAnswerItem{QuestionID: "q0"}),
		"unknown question": ans(QuestionAnswerItem{QuestionID: "zz", OptionIDs: []string{"o0"}}),
		"no answers":       {Action: QuestionActionAnswer},
		"over cap text":    ans(QuestionAnswerItem{QuestionID: "q0", Text: strings.Repeat("x", MaxAnswerText+1)}),
		"long note":        {Action: QuestionActionDecline, Note: strings.Repeat("n", MaxQuestionNote+1)},
		"bad action":       {Action: "maybe"},
	} {
		if err := ok(a); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	noOther := single
	noOther.Questions = []Question{single.Questions[0]}
	noOther.Questions[0].AllowOther = false
	if err := ValidateQuestionAnswer(noOther, ans(QuestionAnswerItem{QuestionID: "q0", Text: "x"})); err == nil {
		t.Fatal("free text accepted where not allowed")
	}
}

type fakeQD struct {
	PermissionDecider
	out QuestionOutcome
	got *QuestionRequest
	err error
}

func (f *fakeQD) Decide(context.Context, string, []PermissionOption) (string, error) {
	panic("AskUserQuestion must not take the plain permission path when a QuestionDecider is present")
}

func (f *fakeQD) DecideQuestion(_ context.Context, r QuestionRequest) (QuestionOutcome, error) {
	f.got = &r
	return f.out, f.err
}

func askReq(t *testing.T, fixture string) claudecode.CanUseToolRequest {
	return claudecode.CanUseToolRequest{ToolName: "AskUserQuestion", Input: loadAskInput(t, fixture)}
}

func TestClaudeAdapter_AskUserQuestionAnswerAllowsWithAnswers(t *testing.T) {
	qd := &fakeQD{out: QuestionOutcome{Resolution: QuestionResolvedByHuman, Answer: QuestionAnswer{Action: QuestionActionAnswer,
		Answers: []QuestionAnswerItem{{QuestionID: "q0", OptionIDs: []string{"o1"}}}}}}
	allow, in, msg, _, _, err := claudeDeciderAdapter{decider: qd}.Decide(context.Background(), askReq(t, "single"))
	if err != nil || !allow || msg != "" {
		t.Fatalf("allow=%v msg=%q err=%v", allow, msg, err)
	}
	if got := in["answers"].(map[string]any)["Which colour do you prefer?"]; got != "Blue" {
		t.Fatalf("answers: %v", in["answers"])
	}
	if qd.got == nil || qd.got.Source != QuestionSourceClaude {
		t.Fatalf("request: %+v", qd.got)
	}
}

func TestClaudeAdapter_AskUserQuestionDeclineDeniesWithNote(t *testing.T) {
	qd := &fakeQD{out: QuestionOutcome{Resolution: QuestionResolvedTimeout, Answer: QuestionAnswer{Action: QuestionActionDecline, Note: QuestionNoteTimeout}}}
	allow, in, msg, _, _, err := claudeDeciderAdapter{decider: qd}.Decide(context.Background(), askReq(t, "single"))
	if err != nil || allow || in != nil {
		t.Fatalf("allow=%v in=%v err=%v", allow, in, err)
	}
	if !strings.HasPrefix(msg, "The user declined to answer: ") || !strings.Contains(msg, "No human answered") {
		t.Fatalf("deny message: %q", msg)
	}
}

func TestClaudeAdapter_AskUserQuestionMalformedIsDeclinedNotAnswered(t *testing.T) {
	qd := &fakeQD{}
	req := claudecode.CanUseToolRequest{ToolName: "AskUserQuestion", Input: map[string]any{"questions": "nope"}}
	allow, _, msg, _, _, err := claudeDeciderAdapter{decider: qd}.Decide(context.Background(), req)
	if err != nil || allow || msg == "" || qd.got != nil {
		t.Fatalf("allow=%v msg=%q err=%v asked=%v", allow, msg, err, qd.got != nil)
	}
}
