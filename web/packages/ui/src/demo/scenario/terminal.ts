import { DEMO_PROMPT } from "../terminal";
import type { TerminalReplay } from "../types";

/**
 * Canned terminal sessions (ADR-0051): output only, nothing ever runs. The lines are the landing hero's `term` for each task;
 * a line starting with `$ ` is a command and is drawn after the demo prompt, the rest is its output.
 */
function replay(lines: string[]): TerminalReplay {
  return {
    frames: lines.map((line, i) => {
      const command = line.startsWith("$ ");
      return { afterMs: command ? (i === 0 ? 200 : 450) : 280, text: command ? `${DEMO_PROMPT}${line.slice(2)}\r\n` : `${line}\r\n` };
    }),
  };
}

export const TERMINALS: Record<number, TerminalReplay> = {
  1: replay(["$ go test ./internal/webhook/...", "ok  \tcheckout/internal/webhook\t0.288s"]),
  2: replay(["$ go test -race -count=20 ./internal/retry/...", "ok  \tcheckout/internal/retry\t3.904s"]),
  3: replay(["$ go test ./internal/dates/...", "ok  \tcheckout/internal/dates\t0.021s"]),
  4: replay(["$ gh pr diff 42 --name-only", "internal/cache/store.go", "internal/worker/pool.go"]),
  5: replay(["$ go tool pprof -top cpu.prof", "      flat  flat%   sum%  cum  name", "   1.20s 31.4% 31.4%  1.9s  database/sql.(*DB).conn"]),
  6: replay(["$ tree -L 2 src", "src", "├── app", "├── components", "└── lib"]),
  12: replay(["$ git log --oneline docs/adr", "c41e9d2 docs: job queue ADR draft"]),
};
