// Synthetic worktree diffs and files of the demo tour (ADR-0051). Nothing here is real code or a real path. The diff sizes are the
// landing hero's (+41 −0, +2 −1, +24 −0); scenario.test.ts pins them.

/** A new-file diff: every line of `body` added. */
function newFile(path: string, body: string[]): string {
  return [
    `diff --git a/${path} b/${path}`,
    "new file mode 100644",
    "index 0000000..e5f0a1b",
    "--- /dev/null",
    `+++ b/${path}`,
    `@@ -0,0 +1,${body.length} @@`,
    ...body.map((l) => `+${l}`),
    "",
  ].join("\n");
}

/** Webhook (the live task): the limiter file the permission request is about, appended once the visitor allows it. +41 −0. */
export const LIVE_EXTRA_DIFF = newFile("internal/webhook/limiter.go", [
  "package webhook",
  "",
  "import (",
  '\t"sync"',
  '\t"time"',
  ")",
  "",
  "// Limiter is a token bucket: rate tokens per second, burst at most.",
  "// Tokens refill continuously at rate per second, up to burst.",
  "// The zero Limiter denies everything; use NewLimiter.",
  "type Limiter struct {",
  "\tmu     sync.Mutex",
  "\ttokens float64",
  "\tlast   time.Time",
  "\trate   float64",
  "\tburst  float64",
  "}",
  "",
  "// NewLimiter returns a full bucket.",
  "func NewLimiter(rate float64, burst int) *Limiter {",
  "\treturn &Limiter{tokens: float64(burst), rate: rate, burst: float64(burst)}",
  "}",
  "",
  "// Allow takes one token if there is one, and says how long to wait if not.",
  "func (l *Limiter) Allow(now time.Time) (bool, time.Duration) {",
  "\tl.mu.Lock()",
  "\tdefer l.mu.Unlock()",
  "\tif !l.last.IsZero() {",
  "\t\tl.tokens += now.Sub(l.last).Seconds() * l.rate",
  "\t\tif l.tokens > l.burst {",
  "\t\t\tl.tokens = l.burst",
  "\t\t}",
  "\t}",
  "\tl.last = now",
  "\tif l.tokens >= 1 {",
  "\t\tl.tokens--",
  "\t\treturn true, 0",
  "\t}",
  "\twait := time.Duration((1 - l.tokens) / l.rate * float64(time.Second))",
  "\treturn false, wait",
  "}",
]);

/** Debug (TestRetry): pin the jitter seed. +2 −1. */
const RETRY_DIFF = `diff --git a/internal/retry/backoff.go b/internal/retry/backoff.go
index 3b1c2f0..9a7d4e1 100644
--- a/internal/retry/backoff.go
+++ b/internal/retry/backoff.go
@@ -14,4 +14,5 @@ func jitter(attempt int) time.Duration {
 func jitter(attempt int) time.Duration {
-	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
+	seed := testSeed()
+	rng := rand.New(rand.NewSource(seed))
 	return time.Duration(rng.Int63n(int64(base << attempt)))
 }
`;

/** First task (parseDate): the new table test. +24 −0. */
const PARSE_DATE_DIFF = newFile("internal/dates/parse_test.go", [
  "package dates",
  "",
  'import "testing"',
  "",
  "func TestParseDate(t *testing.T) {",
  "\tcases := []struct {",
  "\t\tname string",
  "\t\tin   string",
  "\t\tok   bool",
  "\t}{",
  '\t\t{"iso", "2026-02-27", true},',
  '\t\t{"rfc3339", "2026-02-27T10:00:00+00:00", true},',
  '\t\t{"empty", "", false},',
  '\t\t{"bad month", "2026-13-01", false},',
  '\t\t{"leap day", "2028-02-29", true},',
  "\t}",
  "\tfor _, c := range cases {",
  "\t\tt.Run(c.name, func(t *testing.T) {",
  "\t\t\tif _, err := parseDate(c.in); (err == nil) != c.ok {",
  '\t\t\t\tt.Fatalf("parseDate(%q) error = %v, want ok=%v", c.in, err, c.ok)',
  "\t\t\t}",
  "\t\t})",
  "\t}",
  "}",
]);

/** task.diff text by task id. The webhook task (1) has none until its permission is allowed. */
export const DIFFS: Record<number, string> = {
  2: RETRY_DIFF,
  3: PARSE_DATE_DIFF,
};

export const FILES: Record<number, Record<string, string>> = {
  1: {
    "README.md": "# checkout-api\n\nThe checkout service of the Acme demo.\n",
    "internal/webhook/handler.go": "package webhook\n\n// Handler receives provider webhooks.\ntype Handler struct{}\n",
  },
  2: {
    "internal/retry/backoff.go": "package retry\n\nfunc jitter(attempt int) time.Duration {\n\treturn 0\n}\n",
  },
  3: {
    "internal/dates/parse.go": "package dates\n\nfunc parseDate(s string) (time.Time, error) { return time.Time{}, nil }\n",
  },
  6: {
    "README.md": "# web-app\n\nThe customer dashboard.\n",
    "src/app/page.tsx": "export default function Page() {\n  return null;\n}\n",
  },
  12: {
    "docs/adr/job-queue.md": "# Job queue\n\n## Schema\n\n## Failure modes\n",
  },
};
