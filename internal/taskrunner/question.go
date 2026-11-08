package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// This file is ADR-0055's provider-agnostic question model: what an agent's
// "ask the human" tool call becomes inside smind, the daemon-side caps that
// make its (untrusted) text safe to render, the secret refusal, and the
// answer validation. Provider mapping lives next to each provider's adapter
// (claudeDeciderAdapter for Claude Code's AskUserQuestion).

// Question limits (ADR-0055 §1). Enforced when a request is built, never in
// the UI alone.
const (
	maxQuestions         = 4
	maxQuestionOptions   = 8
	maxQuestionHeader    = 32
	maxQuestionText      = 1000
	maxOptionLabel       = 80
	maxOptionDesc        = 200
	MaxAnswerText        = 2000
	MaxQuestionNote      = 500
	QuestionSourceClaude = "claude.ask_user_question"
)

// QuestionKind is how a question is answered.
type QuestionKind string

const (
	QuestionSingle QuestionKind = "single"
	QuestionMulti  QuestionKind = "multi"
	QuestionText   QuestionKind = "text"
)

// QuestionOption is one choice of a single/multi question. ID is smind's own
// opaque key ("o<index>"); the provider's text is Label/Description.
type QuestionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`

	// orig is the label exactly as the agent wrote it (Label may be capped for
	// display); an answer is sent back under the original so the agent gets the
	// option it offered, not a shortened copy.
	orig string
}

// Question is one question of a request.
type Question struct {
	ID         string           `json:"id"`
	Header     string           `json:"header,omitempty"`
	Text       string           `json:"text"`
	Kind       QuestionKind     `json:"kind"`
	Options    []QuestionOption `json:"options,omitempty"`
	AllowOther bool             `json:"allowOther,omitempty"`
}

// QuestionRequest is what a provider adapter hands a QuestionDecider.
type QuestionRequest struct {
	Source    string     `json:"source"`
	Questions []Question `json:"questions"`
	// Truncated is true when a cap dropped or shortened anything, so the card
	// can say the text is not complete.
	Truncated bool `json:"truncated,omitempty"`

	// secret is set while parsing, on the agent's FULL text before any cap, when
	// a header, question or option text asks for a secret (see AsksForSecret).
	secret bool
}

// QuestionAnswer actions.
const (
	QuestionActionAnswer  = "answer"
	QuestionActionDecline = "decline"
)

// QuestionAnswerItem answers one question: chosen option ids and/or free text.
type QuestionAnswerItem struct {
	QuestionID string   `json:"questionId"`
	OptionIDs  []string `json:"optionIds,omitempty"`
	Text       string   `json:"text,omitempty"`
}

// QuestionAnswer is a human's answer (or decline) to a QuestionRequest.
type QuestionAnswer struct {
	Action  string               `json:"action"`
	Answers []QuestionAnswerItem `json:"answers,omitempty"`
	// Note is the human's reason for a decline (<= MaxQuestionNote); it goes
	// back to the agent.
	Note string `json:"note,omitempty"`
}

// QuestionResolution says how a question request ended (the wire "reason" of
// question_resolved).
type QuestionResolution string

const (
	QuestionResolvedByHuman      QuestionResolution = "human"
	QuestionResolvedDeclined     QuestionResolution = "declined"
	QuestionResolvedTimeout      QuestionResolution = "timeout"
	QuestionResolvedSecret       QuestionResolution = "refused_secret"
	QuestionResolvedUnsupported  QuestionResolution = "refused_unsupported"
	QuestionResolvedRepeat       QuestionResolution = "repeat"
	QuestionResolvedProfile      QuestionResolution = "profile_decline"
	QuestionResolvedNoHuman      QuestionResolution = "no_human"
	QuestionResolvedProviderStop QuestionResolution = "provider_cancellation"
	QuestionResolvedRunEnded     QuestionResolution = "run_ended"
	// QuestionResolvedDaemonRestart closes a question a restart orphaned.
	QuestionResolvedDaemonRestart QuestionResolution = "daemon_restart"
)

// QuestionOutcome is a QuestionDecider's result: the answer to forward to the
// agent (a decline carries the note the agent reads) and how it was reached.
type QuestionOutcome struct {
	Answer     QuestionAnswer
	Resolution QuestionResolution
}

// QuestionDecider is the optional extension of PermissionDecider that
// parks a question for a human. A decider that does not implement it makes an
// adapter fall back to its plain permission path (the pre-ADR-0055 behaviour).
// It never returns an answer nobody gave: a timeout, a policy or a refusal is
// a decline with a note (ADR-0055 §5).
type QuestionDecider interface {
	DecideQuestion(ctx context.Context, req QuestionRequest) (QuestionOutcome, error)
}

// Standing decline notes the agent reads.
const (
	QuestionNoteSecret  = "Secrets cannot be provided through a question. Ask the human to set it in the environment or the provider's own login."
	QuestionNoteProfile = "This run is not attended: questions are declined. Proceed with your best judgement or stop and report what you need."
	QuestionNoteNoHuman = "No human is attached to this run."
	QuestionNoteRepeat  = "That question was already asked recently; do not ask it again. Proceed with your best judgement or stop and report what you need."
	QuestionNoteBadForm = "This question could not be shown to the human (unsupported or malformed). Proceed with your best judgement."
)

// secretVocab is the small word list that marks a question as asking for a
// secret (ADR-0055 §6). A false positive costs a decline with a clear note.
var secretVocab = regexp.MustCompile(`(?i)\b(passwords?|passphrases?|passcodes?|secrets?|api[ _-]?keys?|access[ _-]?tokens?|auth(?:entication)?[ _-]?tokens?|bearer|tokens?|credentials?|private[ _-]?keys?|ssh[ _-]?keys?|2fa|otp)\b`)

// secretHomoglyphs folds the lookalike letters (Cyrillic, Greek) an agent can
// use to dodge the vocabulary into the Latin letter they imitate.
var secretHomoglyphs = map[rune]rune{
	'а': 'a', 'с': 'c', 'е': 'e', 'о': 'o', 'р': 'p', 'х': 'x', 'у': 'y', 'і': 'i', 'ј': 'j', 'ѕ': 's',
	'ԁ': 'd', 'һ': 'h', 'ԛ': 'q', 'ԝ': 'w', 'к': 'k', 'м': 'm', 'т': 't', 'ӏ': 'l',
	'α': 'a', 'ε': 'e', 'ο': 'o', 'ρ': 'p', 'υ': 'u', 'ν': 'v', 'ι': 'i', 'κ': 'k', 'τ': 't', 'χ': 'x',
}

// secretSkeleton reduces s to a plain lowercase Latin skeleton before the
// vocabulary is matched: compatibility-decomposed (full-width and styled
// letters), with combining marks, controls and format characters (zero-width
// joiners, bidi marks) removed, and common homoglyphs folded.
func secretSkeleton(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Cf, r) || unicode.IsControl(r) {
			continue
		}
		r = unicode.ToLower(r)
		if f, ok := secretHomoglyphs[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

func mentionsSecret(s string) bool { return secretVocab.MatchString(secretSkeleton(s)) }

// MentionsSecret reports whether s matches the secret vocabulary after the
// same normalisation AsksForSecret uses (for callers deciding what to persist).
func MentionsSecret(s string) bool { return mentionsSecret(s) }

// AsksForSecret reports whether any header, text, option label or option
// description of req matches the secret vocabulary after normalisation. The
// request builder also checks the agent's full text before it is capped, so a
// secret word past a cap cannot hide. It is a refusal heuristic, not a
// boundary.
func AsksForSecret(req QuestionRequest) bool {
	if req.secret {
		return true
	}
	for _, q := range req.Questions {
		if mentionsSecret(q.Header) || mentionsSecret(q.Text) {
			return true
		}
		for _, o := range q.Options {
			if mentionsSecret(o.Label) || mentionsSecret(o.Description) {
				return true
			}
		}
	}
	return false
}

// capQuestionText shortens s to max runes with an ellipsis and says whether it did.
// It also drops control and format characters (other than newline and tab) and
// maps line/paragraph separators to newline, so a card, a terminal or a log
// cannot be made to render escape sequences, reordered text or hidden characters.
func capQuestionText(s string, max int) (string, bool) {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\u2028' || r == '\u2029':
			return '\n'
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// C0, DEL and C1 controls (including CSI U+009B), and format
			// characters: bidi overrides and isolates, zero-width characters.
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= max {
		return s, false
	}
	rs := []rune(s)
	return string(rs[:max-1]) + "…", true
}

// ParseClaudeAskUserQuestion builds a QuestionRequest from Claude Code's
// AskUserQuestion tool input ({questions:[{question, header, multiSelect,
// options:[{label, description}]}]}). An input that does not fit (no
// questions, more than maxQuestions, a question without text or without any
// option) is an error: the caller declines the whole request.
func ParseClaudeAskUserQuestion(input map[string]any) (QuestionRequest, error) {
	req := QuestionRequest{Source: QuestionSourceClaude}
	raw, ok := input["questions"].([]any)
	if !ok || len(raw) == 0 {
		return req, errors.New("no questions")
	}
	if len(raw) > maxQuestions {
		return req, fmt.Errorf("%d questions (max %d)", len(raw), maxQuestions)
	}
	for i, rq := range raw {
		m, ok := rq.(map[string]any)
		if !ok {
			return req, fmt.Errorf("question %d is not an object", i)
		}
		text, _ := m["question"].(string)
		if strings.TrimSpace(text) == "" {
			return req, fmt.Errorf("question %d has no text", i)
		}
		q := Question{ID: fmt.Sprintf("q%d", i), Kind: QuestionSingle, AllowOther: true}
		var cut bool
		req.secret = req.secret || mentionsSecret(text)
		if h, ok := m["header"].(string); ok {
			req.secret = req.secret || mentionsSecret(h)
		}
		q.Text, cut = capQuestionText(text, maxQuestionText)
		req.Truncated = req.Truncated || cut
		if h, ok := m["header"].(string); ok {
			q.Header, cut = capQuestionText(h, maxQuestionHeader)
			req.Truncated = req.Truncated || cut
		}
		if multi, _ := m["multiSelect"].(bool); multi {
			q.Kind = QuestionMulti
		}
		opts, _ := m["options"].([]any)
		if len(opts) == 0 {
			return req, fmt.Errorf("question %d has no options", i)
		}
		for j, ro := range opts {
			if j >= maxQuestionOptions {
				req.Truncated = true
				break
			}
			om, ok := ro.(map[string]any)
			if !ok {
				return req, fmt.Errorf("question %d option %d is not an object", i, j)
			}
			label, _ := om["label"].(string)
			if strings.TrimSpace(label) == "" {
				return req, fmt.Errorf("question %d option %d has no label", i, j)
			}
			o := QuestionOption{ID: fmt.Sprintf("o%d", j), orig: label}
			req.secret = req.secret || mentionsSecret(label)
			if d, ok := om["description"].(string); ok {
				req.secret = req.secret || mentionsSecret(d)
			}
			o.Label, cut = capQuestionText(label, maxOptionLabel)
			req.Truncated = req.Truncated || cut
			if d, ok := om["description"].(string); ok {
				o.Description, cut = capQuestionText(d, maxOptionDesc)
				req.Truncated = req.Truncated || cut
			}
			q.Options = append(q.Options, o)
		}
		req.Questions = append(req.Questions, q)
	}
	return req, nil
}

// ValidateQuestionAnswer checks a human's answer against req: a decline needs
// nothing; an answer must cover every question exactly once with known option
// ids, and free text only where the question takes it.
func ValidateQuestionAnswer(req QuestionRequest, a QuestionAnswer) error {
	if utf8.RuneCountInString(a.Note) > MaxQuestionNote {
		return fmt.Errorf("note is longer than %d characters", MaxQuestionNote)
	}
	switch a.Action {
	case QuestionActionDecline:
		return nil
	case QuestionActionAnswer:
	default:
		return fmt.Errorf("unknown action %q (want answer or decline)", a.Action)
	}
	byQ := make(map[string]QuestionAnswerItem, len(a.Answers))
	for _, it := range a.Answers {
		if _, dup := byQ[it.QuestionID]; dup {
			return fmt.Errorf("question %q answered twice", it.QuestionID)
		}
		byQ[it.QuestionID] = it
	}
	if len(byQ) != len(req.Questions) {
		return fmt.Errorf("need an answer for each of %d questions, got %d", len(req.Questions), len(byQ))
	}
	for _, q := range req.Questions {
		it, ok := byQ[q.ID]
		if !ok {
			return fmt.Errorf("question %q is not answered", q.ID)
		}
		if utf8.RuneCountInString(it.Text) > MaxAnswerText {
			return fmt.Errorf("question %q: text is longer than %d characters", q.ID, MaxAnswerText)
		}
		text := strings.TrimSpace(it.Text)
		known := make(map[string]bool, len(q.Options))
		for _, o := range q.Options {
			known[o.ID] = true
		}
		seen := map[string]bool{}
		for _, id := range it.OptionIDs {
			if !known[id] {
				return fmt.Errorf("question %q: unknown option %q", q.ID, id)
			}
			if seen[id] {
				return fmt.Errorf("question %q: option %q chosen twice", q.ID, id)
			}
			seen[id] = true
		}
		if text != "" && q.Kind != QuestionText && !q.AllowOther {
			return fmt.Errorf("question %q does not take free text", q.ID)
		}
		switch q.Kind {
		case QuestionSingle:
			if len(it.OptionIDs)+boolInt(text != "") != 1 {
				return fmt.Errorf("question %q needs exactly one choice or one free text", q.ID)
			}
		case QuestionMulti:
			if len(it.OptionIDs) == 0 && text == "" {
				return fmt.Errorf("question %q needs at least one choice", q.ID)
			}
		case QuestionText:
			if text == "" || len(it.OptionIDs) > 0 {
				return fmt.Errorf("question %q needs free text", q.ID)
			}
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// claudeAnswerValue is the answer string Claude Code's AskUserQuestion
// expects for one question: the chosen label (several labels of a
// multi-select joined with ", "), or the typed text.
func claudeAnswerValue(q Question, it QuestionAnswerItem) string {
	labels := make([]string, 0, len(it.OptionIDs)+1)
	for _, id := range it.OptionIDs {
		for _, o := range q.Options {
			if o.ID == id {
				if o.orig != "" {
					labels = append(labels, o.orig)
				} else {
					labels = append(labels, o.Label)
				}
			}
		}
	}
	if t := strings.TrimSpace(it.Text); t != "" {
		labels = append(labels, t)
	}
	return strings.Join(labels, ", ")
}

// ClaudeAnswersInput is the updatedInput a Claude Code AskUserQuestion allow
// carries: the original tool input plus "answers", a map from the question's
// text to its answer value. orig is not modified. The question text used as a
// key is the agent's original text (not the capped one) so the CLI matches it.
func ClaudeAnswersInput(orig map[string]any, req QuestionRequest, a QuestionAnswer) map[string]any {
	out := make(map[string]any, len(orig)+1)
	for k, v := range orig {
		out[k] = v
	}
	rawQs, _ := orig["questions"].([]any)
	answers := map[string]any{}
	byQ := make(map[string]QuestionAnswerItem, len(a.Answers))
	for _, it := range a.Answers {
		byQ[it.QuestionID] = it
	}
	for i, q := range req.Questions {
		key := q.Text
		if i < len(rawQs) {
			if m, ok := rawQs[i].(map[string]any); ok {
				if t, ok := m["question"].(string); ok {
					key = t
				}
			}
		}
		answers[key] = claudeAnswerValue(q, byQ[q.ID])
	}
	out["answers"] = answers
	return out
}

// ClaudeDeclineMessage is the deny message the model reads for a declined
// AskUserQuestion.
func ClaudeDeclineMessage(note string) string {
	if strings.TrimSpace(note) == "" {
		return "The user declined to answer."
	}
	return "The user declined to answer: " + note
}

// QuestionEvent is the payload of the question run events. A request event
// carries Request; a resolved event carries Resolution and, for a human
// answer, Answer. RequestID correlates the two.
type QuestionEvent struct {
	RequestID  string             `json:"requestId"`
	Request    *QuestionRequest   `json:"request,omitempty"`
	Resolution QuestionResolution `json:"resolution,omitempty"`
	Answer     *QuestionAnswer    `json:"answer,omitempty"`
}

// QuestionNoteTimeout is the decline note for a question nobody answered
// within d (the run's real permission timeout, not a fixed figure).
func QuestionNoteTimeout(d time.Duration) string {
	return "No human answered within " + humanDuration(d) + "; proceed with your best judgement or stop and report what you need."
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= time.Minute && d%time.Minute == 0:
		if n := int(d / time.Minute); n == 1 {
			return "1 minute"
		} else {
			return fmt.Sprintf("%d minutes", n)
		}
	case d >= time.Second && d%time.Second == 0:
		return fmt.Sprintf("%d seconds", int(d/time.Second))
	}
	return d.Round(time.Millisecond).String()
}
