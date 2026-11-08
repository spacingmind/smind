package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
)

// cmdTaskAnswerUsage is printed on any argument error in cmdTaskAnswer.
const cmdTaskAnswerUsage = `usage: smind task answer <runId> [requestId] [--answer <questionId>=<optionNumber|optionLabel|text>]... [--decline] [--note <text>]`

// maxAnswerNoteRunes mirrors the daemon's cap on a decline note.
const maxAnswerNoteRunes = 500

// answerStdinIsTerminal and answerInput are package vars so tests can drive
// the interactive prompt without a real terminal.
var (
	answerStdinIsTerminal = func() bool {
		return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	}
	answerInput io.Reader = os.Stdin
)

// questionAnswer is one entry of run.answerQuestion's answers array.
type questionAnswer struct {
	QuestionID string   `json:"questionId"`
	OptionIDs  []string `json:"optionIds"`
	Text       string   `json:"text,omitempty"`
}

// cmdTaskAnswer is the human-only way to answer (or decline) an agent's
// question from the CLI (ADR-0055). There is deliberately no MCP tool for
// it: an agent must not answer its own question.
func cmdTaskAnswer(args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		fmt.Println(cmdTaskAnswerUsage)
		return 0
	}
	// A question is answered by a human, never by the agent that asked it: an
	// agent shell inherits SMIND_RUN_ID, so refuse there (same guard as
	// `smind profile seed`). This is a speed bump, not a boundary: the daemon
	// treats an unidentified local connection as a human, so the policy engine
	// should also deny the command to agents (open item in the plan).
	if os.Getenv("SMIND_RUN_ID") != "" {
		fmt.Fprintln(os.Stderr, "task answer is for humans: a human must answer an agent's question, not the agent")
		return 1
	}
	var (
		positional []string
		rawAnswers []string
		decline    bool
		note       string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--decline":
			decline = true
		case a == "--answer" || a == "--note":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, cmdTaskAnswerUsage)
				return 2
			}
			i++
			if a == "--answer" {
				rawAnswers = append(rawAnswers, args[i])
			} else {
				note = args[i]
			}
		case strings.HasPrefix(a, "--answer="):
			rawAnswers = append(rawAnswers, strings.TrimPrefix(a, "--answer="))
		case strings.HasPrefix(a, "--note="):
			note = strings.TrimPrefix(a, "--note=")
		case strings.HasPrefix(a, "--"):
			fmt.Fprintln(os.Stderr, cmdTaskAnswerUsage)
			return 2
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) < 1 || len(positional) > 2 || (decline && len(rawAnswers) > 0) || (note != "" && !decline) {
		fmt.Fprintln(os.Stderr, cmdTaskAnswerUsage)
		return 2
	}
	if len([]rune(note)) > maxAnswerNoteRunes {
		fmt.Fprintf(os.Stderr, "task answer: --note is longer than %d characters\n", maxAnswerNoteRunes)
		return 1
	}
	interactive := !decline && len(rawAnswers) == 0
	if interactive && !answerStdinIsTerminal() {
		fmt.Fprintln(os.Stderr, cmdTaskAnswerUsage)
		return 2
	}
	runID := positional[0]

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer client.Close()

	pending, err := fetchPendingQuestions(context.Background(), client, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task answer: %v\n", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintln(os.Stderr, "task answer: no pending question")
		return 1
	}
	target := &pending[0]
	if len(positional) == 2 {
		target = nil
		for i := range pending {
			if pending[i].RequestID == positional[1] {
				target = &pending[i]
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "task answer: request %q is not pending\n", positional[1])
			return 1
		}
	}

	var answers []questionAnswer
	switch {
	case decline:
	case interactive:
		fmt.Print(renderQuestionRequest(runID, target.RequestID, target.Question))
		answers, decline, err = promptAnswers(bufio.NewReader(answerInput), target.Question)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task answer: %v\n", err)
			return 1
		}
	default:
		answers, err = answersFromFlags(target.Question, rawAnswers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task answer: %v\n", err)
			return 1
		}
	}

	params := map[string]any{"runId": runID, "requestId": target.RequestID}
	verb := "answered"
	if decline {
		verb = "declined"
		params["action"] = "decline"
		if note != "" {
			params["note"] = note
		}
	} else {
		params["action"] = "answer"
		params["answers"] = answers
	}
	if err := client.Call(context.Background(), "run.answerQuestion", params, nil); err != nil {
		fmt.Fprintf(os.Stderr, "task answer: %v\n", err)
		return 1
	}
	fmt.Printf("%s %s\n", verb, target.RequestID)
	return 0
}

// answersFromFlags resolves every --answer <questionId>=<value> against the
// question, and errors (before any daemon call) on an unknown question id,
// a value that matches nothing, or a question left unanswered.
func answersFromFlags(q questionRequest, raw []string) ([]questionAnswer, error) {
	byID := map[string]questionItem{}
	for _, item := range q.Questions {
		byID[item.ID] = item
	}
	values := map[string]string{}
	for _, r := range raw {
		id, value, ok := strings.Cut(r, "=")
		if !ok {
			return nil, fmt.Errorf("--answer %q: want <questionId>=<value>", r)
		}
		if _, known := byID[id]; !known {
			return nil, fmt.Errorf("--answer %q: no question with id %q", r, id)
		}
		values[id] = value
	}
	var missing []string
	for _, item := range q.Questions {
		if _, ok := values[item.ID]; !ok {
			missing = append(missing, item.ID)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing answer for question %s", strings.Join(missing, ", "))
	}
	answers := make([]questionAnswer, 0, len(q.Questions))
	for _, item := range q.Questions {
		a, err := resolveAnswer(item, values[item.ID])
		if err != nil {
			return nil, err
		}
		answers = append(answers, a)
	}
	return answers, nil
}

// resolveAnswer maps one answer value onto a question: a text question takes
// the value verbatim; otherwise numbers (or a comma list of them) select
// options by 1-based position, else an exact case-insensitive label matches,
// else the value is free text if the question allows other.
func resolveAnswer(item questionItem, value string) (questionAnswer, error) {
	a := questionAnswer{QuestionID: item.ID, OptionIDs: []string{}}
	if item.Kind == "text" {
		a.Text = value
		return a, nil
	}
	if ids, ok := matchOptions(item, value); ok {
		a.OptionIDs = ids
		return a, nil
	}
	if item.AllowOther {
		a.Text = value
		return a, nil
	}
	return a, fmt.Errorf("question %s: no option matches %q", item.ID, value)
}

// matchOptions resolves value to option ids: the whole value as a label,
// else every comma-separated token as a 1-based number or a label. A single
// question accepts exactly one option.
func matchOptions(item questionItem, value string) ([]string, bool) {
	one := func(tok string) (string, bool) {
		tok = strings.TrimSpace(tok)
		if n, err := strconv.Atoi(tok); err == nil {
			if n >= 1 && n <= len(item.Options) {
				return item.Options[n-1].ID, true
			}
			return "", false
		}
		for _, o := range item.Options {
			if strings.EqualFold(o.Label, tok) {
				return o.ID, true
			}
		}
		return "", false
	}
	if id, ok := one(value); ok {
		return []string{id}, true
	}
	if item.Kind != "multi" {
		return nil, false
	}
	var ids []string
	for _, tok := range strings.Split(value, ",") {
		id, ok := one(tok)
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, len(ids) > 0
}

// promptAnswers reads one answer per question from in, re-prompting on
// empty or unmatched input. A line `decline` declines the whole request.
func promptAnswers(in *bufio.Reader, q questionRequest) (answers []questionAnswer, decline bool, err error) {
	for _, item := range q.Questions {
		for {
			fmt.Printf("answer for %s> ", item.ID)
			line, rerr := in.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" && rerr != nil {
				return nil, false, fmt.Errorf("no answer for question %s: input ended", item.ID)
			}
			if line == "" {
				continue
			}
			if line == "decline" {
				return nil, true, nil
			}
			a, aerr := resolveAnswer(item, line)
			if aerr != nil {
				fmt.Println(aerr)
				if rerr != nil {
					return nil, false, aerr
				}
				continue
			}
			answers = append(answers, a)
			break
		}
	}
	return answers, false, nil
}
