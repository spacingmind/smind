package taskrunner

import (
	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// The Claude client reports its CLI's process id (SDK v0.4.3, Client.PID): this
// line does not compile against an SDK that cannot, so the claude path's browser
// (a socket bound to that process) can never silently depend on a missing method.
var _ interface{ PID() int } = (*claudecode.Client)(nil)

// claudeClientReportsPID is true: the pinned SDK reports the CLI's pid. It stays a
// function so Runner.claudeReportsPID (which tests set for a fake) has one default.
func claudeClientReportsPID() bool { return true }
