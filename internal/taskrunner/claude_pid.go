package taskrunner

import (
	"reflect"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// claudeClientReportsPID reports whether the SDK's Client has a PID method (added
// in v0.4.3). A reflection check, not a method call, so smind builds against an
// SDK with or without it and the claude path's browser turns on exactly when the
// pinned SDK can say which process the CLI is.
func claudeClientReportsPID() bool {
	_, ok := reflect.TypeOf((*claudecode.Client)(nil)).MethodByName("PID")
	return ok
}
