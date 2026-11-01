// Synthetic worktree diffs and files of the demo tour (ADR-0051). Nothing here is real code or a real path.

const RATE_ROUTER = `diff --git a/cmd/api/router.go b/cmd/api/router.go
index 3b1c2f0..9a7d4e1 100644
--- a/cmd/api/router.go
+++ b/cmd/api/router.go
@@ -14,6 +14,7 @@ func NewRouter(cfg Config) http.Handler {
 	mux := http.NewServeMux()
 	mux.Handle("/v1/", http.StripPrefix("/v1", v1(cfg)))

-	return middleware.Auth(cfg.Keys)(mux)
+	limited := ratelimit.Middleware(ratelimit.New(cfg.RateLimit))(mux)
+	return middleware.Auth(cfg.Keys)(limited)
 }
`;

const RATE_CONFIG = `diff --git a/cmd/api/config.go b/cmd/api/config.go
index 6e0f8a2..c41b9d7 100644
--- a/cmd/api/config.go
+++ b/cmd/api/config.go
@@ -9,6 +9,13 @@ type Config struct {
 	Addr string
 	Keys KeyStore
+
+	// RateLimit is applied per API key; zero values mean "use the defaults".
+	RateLimit ratelimit.Config
 }

 func LoadConfig() (Config, error) {
@@ -24,5 +31,9 @@ func LoadConfig() (Config, error) {
 	cfg.Addr = envOr("ADDR", ":8080")
+	cfg.RateLimit = ratelimit.Config{
+		RequestsPerMinute: envInt("RATE_LIMIT_RPM", 100),
+		Burst:             envInt("RATE_LIMIT_BURST", 20),
+	}
 	return cfg, nil
 }
`;

const RATE_TEST = `diff --git a/internal/middleware/auth_test.go b/internal/middleware/auth_test.go
index 1d2e3f4..5a6b7c8 100644
--- a/internal/middleware/auth_test.go
+++ b/internal/middleware/auth_test.go
@@ -31,3 +31,12 @@ func TestAuthRejectsMissingKey(t *testing.T) {
 		t.Fatalf("status = %d, want 401", rec.Code)
 	}
 }
+
+func TestAuthExposesKeyIDToLaterMiddleware(t *testing.T) {
+	var got string
+	h := Auth(testKeys)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
+		got = KeyID(r.Context())
+	}))
+	h.ServeHTTP(httptest.NewRecorder(), authedRequest("k_live_demo"))
+	if got != "demo" {
+		t.Fatalf("KeyID = %q, want demo", got)
+	}
+}
`;

/** The live task's diff before the visitor answers the permission: two files already edited. */
export const LIVE_DIFF = RATE_ROUTER + RATE_CONFIG + RATE_TEST;

/** Appended once the visitor allows the write: the new limiter file the permission request was about. */
export const LIVE_EXTRA_DIFF = `diff --git a/internal/ratelimit/limiter.go b/internal/ratelimit/limiter.go
new file mode 100644
index 0000000..e5f0a1b
--- /dev/null
+++ b/internal/ratelimit/limiter.go
@@ -0,0 +1,30 @@
+package ratelimit
+
+import (
+	"sync"
+	"time"
+)
+
+// Limiter is a per-key token bucket.
+type Limiter struct {
+	mu      sync.Mutex
+	buckets map[string]*bucket
+	rate    float64 // tokens per second
+	burst   float64
+}
+
+func New(cfg Config) *Limiter {
+	return &Limiter{buckets: map[string]*bucket{}, rate: float64(cfg.RequestsPerMinute) / 60, burst: float64(cfg.Burst)}
+}
+
+// Allow reports whether key may make a request now and, if not, how long to wait.
+func (l *Limiter) Allow(key string, now time.Time) (bool, time.Duration) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	b := l.bucket(key, now)
+	if b.tokens >= 1 {
+		b.tokens--
+		return true, 0
+	}
+	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
+}
`;

const FLAKY_DIFF = `diff --git a/internal/queue/worker_test.go b/internal/queue/worker_test.go
index 8c9d0e1..2f3a4b5 100644
--- a/internal/queue/worker_test.go
+++ b/internal/queue/worker_test.go
@@ -42,9 +42,9 @@ func TestRetryBacksOff(t *testing.T) {
 	w := NewWorker(clock, WithMaxAttempts(3))
 	w.Enqueue(failingJob{})
-	time.Sleep(50 * time.Millisecond)
-	if w.Attempts() != 2 {
-		t.Fatalf("attempts = %d, want 2", w.Attempts())
+	clock.Advance(w.BackoffFor(1))
+	if got := w.Attempts(); got != 2 {
+		t.Fatalf("attempts = %d, want 2", got)
 	}
 }
`;

const DARK_DIFF = `diff --git a/src/settings/SettingsPage.tsx b/src/settings/SettingsPage.tsx
index 4a5b6c7..8d9e0f1 100644
--- a/src/settings/SettingsPage.tsx
+++ b/src/settings/SettingsPage.tsx
@@ -3,7 +3,9 @@ import { Panel } from "../ui/Panel";
 export function SettingsPage() {
+  const { theme, setTheme } = useTheme();
   return (
-    <Panel title="Settings">
+    <Panel title="Settings" data-theme={theme}>
+      <ThemeToggle value={theme} onChange={setTheme} />
       <ProfileForm />
     </Panel>
   );
`;

export const DIFFS: Record<number, string> = {
  1: LIVE_DIFF,
  2: FLAKY_DIFF,
  4: DARK_DIFF,
};

export const FILES: Record<number, Record<string, string>> = {
  1: {
    "README.md": "# acme-api\n\nThe public HTTP API of the Acme demo service.\n",
    "cmd/api/router.go": 'package main\n\nfunc NewRouter(cfg Config) http.Handler {\n\tmux := http.NewServeMux()\n\treturn middleware.Auth(cfg.Keys)(mux)\n}\n',
    "internal/middleware/auth.go": "package middleware\n\n// Auth resolves the API key and stores its id on the request context.\n",
  },
  2: {
    "internal/queue/worker.go": "package queue\n\n// Worker runs jobs with exponential backoff between attempts.\n",
  },
  4: {
    "src/settings/SettingsPage.tsx": 'export function SettingsPage() {\n  return <Panel title="Settings" />;\n}\n',
  },
};
