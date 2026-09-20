package taskrunner

import "testing"

func TestThinkingLevel_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level ThinkingLevel
		want  bool
	}{
		{ThinkingLevelUnspecified, true},
		{ThinkingLevelOff, true},
		{ThinkingLevelStandard, true},
		{ThinkingLevelExtended, true},
		{ThinkingLevel("adaptive"), false},
		{ThinkingLevel("Off"), false},
		{ThinkingLevel("bogus"), false},
	}
	for _, tt := range tests {
		if got := tt.level.IsValid(); got != tt.want {
			t.Errorf("ThinkingLevel(%q).IsValid() = %v, want %v", tt.level, got, tt.want)
		}
	}
}
