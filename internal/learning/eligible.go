// Package learning holds the small predicates shared by the self-learning
// subsystem (ADR-0026): which runs may feed it and which providers can take
// part.
package learning

// Run purposes and task kinds, as stored on runs/tasks.
const (
	PurposeUser    = "user"
	PurposeReflect = "reflect"
	PurposeCurate  = "curate"

	TaskKindTask   = "task"
	TaskKindSystem = "system"
)

// Provider ids; keep in sync with the Provider constants in
// internal/taskrunner/provider.go (not imported to keep this package a leaf).
const (
	providerClaude = "claude-native"
	providerGLM    = "glm"
	providerKimi   = "kimi"
)

// Eligible reports whether a run can feed learning: only a normal user run on
// a normal task. Empty purpose/kind default to user/task so rows written
// before the columns existed still qualify.
func Eligible(purpose, taskKind string) bool {
	if purpose == "" {
		purpose = PurposeUser
	}
	return purpose == PurposeUser && taskKind != TaskKindSystem
}

// EmitsToolEvents reports whether the provider streams tool-call events.
// Unknown providers are assumed not to (fail closed).
func EmitsToolEvents(provider string) bool {
	switch provider {
	case providerClaude, providerGLM, providerKimi:
		return true
	}
	return false
}

// CanReflect reports whether the provider may run the reflector (ADR-0026
// D13): claude-native only.
func CanReflect(provider string) bool { return provider == providerClaude }
