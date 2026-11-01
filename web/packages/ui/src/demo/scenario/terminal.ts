import type { TerminalReplay } from "../types";

const PROMPT = "\x1b[32mdemo@smind\x1b[0m:\x1b[34m~/acme-api\x1b[0m$ ";

/** A canned `go test` session for the live task's Terminal tab (ADR-0051): output only, nothing ever runs. */
export const LIVE_TERMINAL: TerminalReplay = {
  frames: [
    { afterMs: 200, text: `${PROMPT}git status --short\r\n` },
    { afterMs: 250, text: " M cmd/api/config.go\r\n M cmd/api/router.go\r\n M internal/middleware/auth_test.go\r\n" },
    { afterMs: 400, text: `${PROMPT}go test ./...\r\n` },
    { afterMs: 700, text: "?   \tacme/cmd/api\t[no test files]\r\n" },
    { afterMs: 500, text: "ok  \tacme/internal/middleware\t0.412s\r\n" },
    { afterMs: 300, text: "ok  \tacme/internal/queue\t1.108s\r\n" },
    { afterMs: 300, text: "\x1b[32mok\x1b[0m  \tacme/internal/store\t0.287s\r\n" },
  ],
};

export const TERMINALS: Record<number, TerminalReplay> = {
  1: LIVE_TERMINAL,
  2: {
    frames: [
      { afterMs: 200, text: `${PROMPT}go test -count=20 -run TestRetryBacksOff ./internal/queue\r\n` },
      { afterMs: 900, text: "ok  \tacme/internal/queue\t0.731s\r\n" },
    ],
  },
};
