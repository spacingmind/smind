package learning

import "testing"

func TestEligible_ExcludesReflectCurateAndSystemTasks(t *testing.T) {
	cases := []struct {
		purpose, kind string
		want          bool
	}{
		{PurposeUser, TaskKindTask, true},
		{PurposeReflect, TaskKindTask, false},
		{PurposeCurate, TaskKindTask, false},
		{PurposeUser, TaskKindSystem, false},
		{PurposeReflect, TaskKindSystem, false},
		{"", "", true},
		{"", TaskKindTask, true},
		{PurposeUser, "", true},
		{"other", TaskKindTask, false},
	}
	for _, tc := range cases {
		if got := Eligible(tc.purpose, tc.kind); got != tc.want {
			t.Errorf("Eligible(%q,%q)=%v want %v", tc.purpose, tc.kind, got, tc.want)
		}
	}
}

func TestEmitsToolEvents(t *testing.T) {
	for p, want := range map[string]bool{
		"claude-native": true, "glm": true, "kimi": true,
		"codex-native": false, "": false, "mystery": false,
	} {
		if got := EmitsToolEvents(p); got != want {
			t.Errorf("EmitsToolEvents(%q)=%v want %v", p, got, want)
		}
	}
}

func TestCanReflect_ClaudeNativeOnly(t *testing.T) {
	for p, want := range map[string]bool{
		"claude-native": true, "glm": false, "kimi": false, "codex-native": false, "": false,
	} {
		if got := CanReflect(p); got != want {
			t.Errorf("CanReflect(%q)=%v want %v", p, got, want)
		}
	}
}
