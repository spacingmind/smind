package taskrunner

import (
	"strings"
	"testing"
)

func modeIDs(modes []ModeInfo) []string {
	ids := make([]string, len(modes))
	for i, m := range modes {
		ids[i] = m.ID
	}
	return ids
}

func TestSupportedProviders_ModeCatalogs(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "")
	want := map[Provider]struct {
		modes       string
		defaultMode string
		autoAccept  bool
		live        bool
	}{
		ProviderClaudeNative: {"acceptEdits,default,plan,auto,bypassPermissions", "acceptEdits", false, true},
		ProviderCodexNative:  {"auto,full-access", "auto", false, false},
		ProviderGLM:          {"default", "default", true, true},
		ProviderKimi:         {"default", "default", true, true},
	}
	for _, p := range SupportedProviders() {
		w := want[p.ID]
		if got := strings.Join(modeIDs(p.Modes), ","); got != w.modes {
			t.Errorf("%s modes = %s, want %s", p.ID, got, w.modes)
		}
		if p.DefaultMode != w.defaultMode || p.SupportsAutoAccept != w.autoAccept || p.LiveModeSwitch != w.live {
			t.Errorf("%s = default %q autoAccept %v live %v, want %+v", p.ID, p.DefaultMode, p.SupportsAutoAccept, p.LiveModeSwitch, w)
		}
	}
}

func TestClaudeModes_AutoHiddenOnBedrockOrVertex(t *testing.T) {
	for _, env := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
			t.Setenv("CLAUDE_CODE_USE_VERTEX", "")
			t.Setenv(env, "1")
			for _, m := range claudeModes() {
				if m.ID == ClaudeModeAuto {
					t.Fatalf("auto mode offered with %s=1", env)
				}
			}
		})
	}
}

func TestValidatePermissionSettings(t *testing.T) {
	claude, _ := ProviderInfoFor(ProviderClaudeNative)
	codex, _ := ProviderInfoFor(ProviderCodexNative)
	glm, _ := ProviderInfoFor(ProviderGLM)
	discovered := glm
	discovered.ModesDiscovered = true
	discovered.Modes = []ModeInfo{{ID: "default"}, {ID: "accept_edits"}}

	cases := []struct {
		name    string
		catalog ProviderInfo
		s       PermissionSettings
		wantErr string
	}{
		{"empty mode is default", claude, PermissionSettings{}, ""},
		{"known claude mode", claude, PermissionSettings{Mode: "plan"}, ""},
		{"unknown claude mode lists valid ids", claude, PermissionSettings{Mode: "auto-safe"}, "valid: acceptEdits"},
		{"autoAccept on claude", claude, PermissionSettings{AutoAccept: true}, "autoAccept is not supported"},
		{"autoAccept on codex", codex, PermissionSettings{AutoAccept: true}, "autoAccept is not supported"},
		{"autoAccept on glm", glm, PermissionSettings{AutoAccept: true}, ""},
		{"undiscovered acp accepts any id", glm, PermissionSettings{Mode: "whatever"}, ""},
		{"discovered acp rejects unknown id", discovered, PermissionSettings{Mode: "whatever"}, "valid: default, accept_edits"},
		{"discovered acp accepts advertised id", discovered, PermissionSettings{Mode: "accept_edits"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePermissionSettings(tc.catalog, tc.s)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestModeAutoApproves(t *testing.T) {
	claude, _ := ProviderInfoFor(ProviderClaudeNative)
	codex, _ := ProviderInfoFor(ProviderCodexNative)
	glm, _ := ProviderInfoFor(ProviderGLM)
	cases := []struct {
		catalog ProviderInfo
		s       PermissionSettings
		want    bool
	}{
		{claude, PermissionSettings{}, false},
		{claude, PermissionSettings{Mode: ClaudeModeAcceptEdits}, false},
		{claude, PermissionSettings{Mode: ClaudeModeBypass}, true},
		{claude, PermissionSettings{Mode: ClaudeModeAuto}, true},
		{codex, PermissionSettings{Mode: CodexModeFullAccess}, true},
		{codex, PermissionSettings{Mode: CodexModeAuto}, false},
		{glm, PermissionSettings{AutoAccept: true}, true},
		{glm, PermissionSettings{Mode: "bypass_permissions"}, true},
		{glm, PermissionSettings{Mode: "accept_edits"}, false},
	}
	for _, tc := range cases {
		if got := ModeAutoApproves(tc.catalog, tc.s); got != tc.want {
			t.Errorf("ModeAutoApproves(%s, %+v) = %v, want %v", tc.catalog.ID, tc.s, got, tc.want)
		}
	}
}
