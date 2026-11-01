import { REDUCED_MOTION_DELAY_MS, type DemoClock } from "./clock";
import type { TerminalReplay } from "./types";

/** UTF-8 text -> base64, the encoding of every terminal "data" event (ADR-0051: the same wire as the daemon's PTY output). */
export function toBase64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}

export const DEMO_PROMPT = "\x1b[32mdemo@smind\x1b[0m:\x1b[34m~/acme-api\x1b[0m$ ";
const DEMO_NOTE = "\x1b[2mdemo: this terminal replays canned output; nothing runs here\x1b[0m\r\n";

type Listener = (item: { data: string } | { end: true }) => void;

/** A terminal session of the scripted daemon: it replays canned frames, echoes typing locally, and executes nothing. */
export class DemoTerminal {
  readonly chunks: string[] = [];
  status: "running" | "closed" = "running";
  private readonly listeners = new Set<Listener>();
  private cancels: Array<() => void> = [];

  constructor(
    readonly id: string,
    readonly taskId: number,
    readonly startedAt: string,
    clock: DemoClock,
    replay: TerminalReplay,
    reducedMotion: boolean,
  ) {
    let t = 0;
    for (const frame of replay.frames) {
      t += frame.afterMs;
      const wait = reducedMotion ? REDUCED_MOTION_DELAY_MS : t;
      this.cancels.push(clock.after(wait, () => this.emit(frame.text)));
    }
    // After the replay the shell is "waiting" at its prompt.
    const end = reducedMotion ? REDUCED_MOTION_DELAY_MS * (replay.frames.length + 1) : t + 150;
    this.cancels.push(clock.after(end, () => this.emit(DEMO_PROMPT)));
  }

  private emit(text: string): void {
    const data = toBase64(text);
    this.chunks.push(data);
    for (const l of this.listeners) l({ data });
  }

  /** Echoes typed text locally; Enter answers one dim line and a fresh prompt. Nothing is executed. */
  write(input: string): void {
    for (const ch of input) {
      if (ch === "\r" || ch === "\n") this.emit(`\r\n${DEMO_NOTE}${DEMO_PROMPT}`);
      else if (ch === "\x7f") this.emit("\b \b");
      else if (ch >= " ") this.emit(ch);
    }
  }

  attach(listener: Listener): () => void {
    for (const data of this.chunks) listener({ data });
    if (this.status === "closed") {
      listener({ end: true });
      return () => {};
    }
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  close(): void {
    if (this.status === "closed") return;
    this.status = "closed";
    for (const c of this.cancels) c();
    this.cancels = [];
    const ls = [...this.listeners];
    this.listeners.clear();
    for (const l of ls) l({ end: true });
  }
}
