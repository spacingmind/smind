// Package learning holds the small predicates shared by the self-learning
// subsystem (ADR-0026): which runs may feed it and which providers can take
// part.
package learning

import "github.com/spacingmind/smind/internal/providers"

// Run purposes and task kinds, as stored on runs/tasks.
const (
	PurposeUser    = "user"
	PurposeReflect = "reflect"
	PurposeCurate  = "curate"

	TaskKindTask   = "task"
	TaskKindSystem = "system"
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
func EmitsToolEvents(provider string) bool { return providers.ToolEvents(provider) }

// CanReflect reports whether the provider may run the reflector (ADR-0026
// D13): the registry marks which (claude-native only; an instance does not,
// because the reflector runs on the base login).
func CanReflect(provider string) bool { return providers.CanReflect(provider) }
