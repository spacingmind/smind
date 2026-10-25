package taskrunner

import (
	"strings"
	"testing"
)

// TestTailBuffer_RedactedTailIsCappedAndOneLine: only the end of a long stderr
// is kept, capped at cliStderrShown, with secrets scrubbed and newlines flattened.
func TestTailBuffer_RedactedTailIsCappedAndOneLine(t *testing.T) {
	t.Parallel()
	var tb tailBuffer
	_, _ = tb.Write([]byte(strings.Repeat("old line\n", 5000)))
	_, _ = tb.Write([]byte("token=hunter2hunter2\nexact: s3cr3t-value-123\nlast line\n"))
	got := tb.redacted([]string{"s3cr3t-value-123"})
	if len(got) > cliStderrShown+4 {
		t.Fatalf("len = %d, want <= %d", len(got), cliStderrShown)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "hunter2") || strings.Contains(got, "s3cr3t-value-123") {
		t.Fatalf("not flattened/redacted: %q", got[len(got)-120:])
	}
	if !strings.HasSuffix(got, "last line") {
		t.Fatalf("tail lost: %q", got[len(got)-60:])
	}
}

// TestClaudeStreamProbe_NoStreamUsageIsNil: usage is summed over API
// messages from the stream; a stream that reported none gives nil, never zeros.
func TestClaudeStreamProbe_NoStreamUsageIsNil(t *testing.T) {
	t.Parallel()
	if u := newClaudeStreamProbe().partialUsage(); u != nil {
		t.Fatalf("partialUsage() = %+v, want nil", u)
	}
}
