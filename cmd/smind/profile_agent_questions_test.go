package main

import "testing"

// ADR-0055 D4: --agent-questions is an explicit, optional boolean flag.
func TestParseProfileFlags_AgentQuestions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		want *bool
		code int
	}{
		{nil, nil, 0},
		{[]string{"--agent-questions"}, boolPtr(true), 0},
		{[]string{"--agent-questions=true"}, boolPtr(true), 0},
		{[]string{"--agent-questions=false"}, boolPtr(false), 0},
		{[]string{"--agent-questions=maybe"}, nil, 2},
	} {
		f, code := parseProfileFlags("profile add", tc.args, false)
		if code != tc.code {
			t.Errorf("%v: code = %d, want %d", tc.args, code, tc.code)
			continue
		}
		if code == 0 && ((f.agentQuestions == nil) != (tc.want == nil) || (tc.want != nil && *f.agentQuestions != *tc.want)) {
			t.Errorf("%v: agentQuestions = %v, want %v", tc.args, f.agentQuestions, tc.want)
		}
	}
}
