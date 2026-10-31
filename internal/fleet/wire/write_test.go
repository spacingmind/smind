package wire

import (
	"reflect"
	"strings"
	"testing"
)

// The write param shapes are closed: nothing that names a local agent
// configuration (mode, profile, env, cwd, attachments, mcp) or a credential
// or authority concept (grant, secret, token, password, account, path,
// policy, cost, budget, key) may appear on them. The only exceptions are the
// exact fields the protocol mandates: IdempotencyKey (the retry key) and
// Model (the LLM to run, not a permission mode).
func TestFleetCreateTypeHasNoModeEnvCwdFields(t *testing.T) {
	forbidden := []string{
		"mode", "profile", "env", "cwd", "attach", "mcp", "grant", "secret",
		"token", "password", "account", "path", "policy", "cost", "budget", "key",
	}
	// field name -> the forbidden word it is allowed to contain
	allowed := map[string]string{
		"IdempotencyKey": "key",
		"Model":          "mode",
	}
	types := []any{TaskCreateParams{}, TaskSendParams{}, TaskStopParams{}, RunWaitParams{}}
	seen := map[reflect.Type]bool{}
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Name == "Prov" {
				continue
			}
			jsonName := strings.ToLower(strings.Split(f.Tag.Get("json"), ",")[0])
			if jsonName == "" {
				t.Errorf("%s.%s has no json name", rt.Name(), f.Name)
			}
			for _, bad := range forbidden {
				if allowed[f.Name] == bad {
					continue
				}
				if strings.Contains(strings.ToLower(f.Name), bad) || strings.Contains(jsonName, bad) {
					t.Errorf("%s.%s (json %q) contains %q", rt.Name(), f.Name, jsonName, bad)
				}
			}
			walk(f.Type)
		}
	}
	for _, v := range types {
		walk(reflect.TypeOf(v))
	}
}

func TestWireCreateValidate(t *testing.T) {
	valid := TaskCreateParams{
		IdempotencyKey: "k1",
		WorkspaceID:    1,
		Provider:       "anthropic",
		Model:          "claude-3",
		Prompt:         "ship it",
	}
	cases := []struct {
		name    string
		mutate  func(p *TaskCreateParams)
		wantErr string
	}{
		{"missing provider", func(p *TaskCreateParams) { p.Provider = "" }, "provider and model are required"},
		{"blank model", func(p *TaskCreateParams) { p.Model = "   " }, "provider and model are required"},
		{"missing prompt", func(p *TaskCreateParams) { p.Prompt = "" }, "prompt is required"},
		{"oversize prompt", func(p *TaskCreateParams) { p.Prompt = strings.Repeat("x", MaxPromptBytes+1) }, "prompt must be at most"},
		{"branch and baseRef both", func(p *TaskCreateParams) { p.Branch, p.BaseRef = "feature/x", "main" }, "mutually exclusive"},
		{"branch leading dash", func(p *TaskCreateParams) { p.Branch = "-x" }, "branch is not a valid ref name"},
		{"branch dotdot", func(p *TaskCreateParams) { p.Branch = "a..b" }, "branch is not a valid ref name"},
		{"branch space", func(p *TaskCreateParams) { p.Branch = "a b" }, "branch is not a valid ref name"},
		{"branch lock suffix", func(p *TaskCreateParams) { p.Branch = "x.lock" }, "branch is not a valid ref name"},
		{"missing idempotencyKey", func(p *TaskCreateParams) { p.IdempotencyKey = "" }, "idempotencyKey is required"},
		{"workspaceId zero", func(p *TaskCreateParams) { p.WorkspaceID = 0 }, "workspaceId must be > 0"},
		{"valid branch", func(p *TaskCreateParams) { p.Branch = "feature/x" }, ""},
		{"valid baseRef", func(p *TaskCreateParams) { p.BaseRef = "main" }, ""},
		{"ok", func(p *TaskCreateParams) {}, ""},
	}
	for _, tc := range cases {
		p := valid
		tc.mutate(&p)
		err := p.Validate()
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: got %v, want error containing %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestWireSendStopWaitValidate(t *testing.T) {
	send := TaskSendParams{IdempotencyKey: "k1", TaskID: 7, Prompt: "go"}
	if err := send.Validate(); err != nil {
		t.Errorf("valid send: unexpected error %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(p *TaskSendParams)
		want   string
	}{
		{"taskId zero", func(p *TaskSendParams) { p.TaskID = 0 }, "taskId must be > 0"},
		{"empty key", func(p *TaskSendParams) { p.IdempotencyKey = "" }, "idempotencyKey is required"},
		{"empty prompt", func(p *TaskSendParams) { p.Prompt = "" }, "prompt is required"},
	} {
		p := send
		tc.mutate(&p)
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("send %s: got %v, want error containing %q", tc.name, err, tc.want)
		}
	}

	stop := TaskStopParams{TaskID: 7}
	if err := stop.Validate(); err != nil {
		t.Errorf("valid stop: unexpected error %v", err)
	}
	st := stop
	st.TaskID = 0
	if err := st.Validate(); err == nil || !strings.Contains(err.Error(), "taskId must be > 0") {
		t.Errorf("stop taskId zero: got %v, want taskId error", err)
	}

	wait := RunWaitParams{RunID: "r1"}
	if err := wait.Validate(); err != nil {
		t.Errorf("valid wait: unexpected error %v", err)
	}
	w := wait
	w.RunID = ""
	if err := w.Validate(); err == nil || !strings.Contains(err.Error(), "runId is required") {
		t.Errorf("wait empty runId: got %v, want runId error", err)
	}
	w = wait
	w.TimeoutSeconds = -1
	if err := w.Validate(); err == nil || !strings.Contains(err.Error(), "timeoutSeconds must be >= 0") {
		t.Errorf("wait negative timeout: got %v, want timeoutSeconds error", err)
	}
}

func TestWireWriteMethodsAreFleetPrefixed(t *testing.T) {
	reads := map[string]bool{}
	for _, m := range ReadMethods {
		reads[m] = true
	}
	for _, m := range WriteMethods {
		if !strings.HasPrefix(m, "fleet.") {
			t.Errorf("write method %q lacks the fleet. prefix", m)
		}
		if reads[m] {
			t.Errorf("write method %q is also a read method", m)
		}
	}
}
