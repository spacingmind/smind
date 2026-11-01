import { describe, expect, it } from "vitest";

import { PERMISSION_REQUEST } from "./scenario/runs";
import { buildTour } from "./scenario";
import type { Step } from "./types";

const NOW = Date.parse("2026-11-02T10:00:00Z");
const tour = buildTour(NOW);

function stepEvents(steps: Step[]): unknown[] {
  return steps.flatMap((s) => ("event" in s ? [s.event] : "ask" in s ? [s.ask, ...stepEvents(s.allow), ...stepEvents(s.deny)] : []));
}

/** Everything the scenario can put on screen, as one string. */
function everything(): string {
  return JSON.stringify({
    ...tour,
    reply: undefined,
    liveScript: stepEvents(tour.live.script.steps),
    replies: [0, 1].map((n) => stepEvents(tour.reply("hello", n).steps)),
  });
}

describe("demo scenario", () => {
  it("S-01 is internally consistent: every referenced workspace, space, task, chat, run and inbox target exists", () => {
    const ws = new Set(tour.workspaces.map((w) => w.ID));
    const spaces = new Set(tour.spaces.map((s) => s.ID));
    const tasks = new Set(tour.tasks.map((t) => t.ID));
    const chats = new Map(tour.chats.map((c) => [c.ID, c.TaskID]));
    for (const s of tour.spaces) expect(ws.has(s.WorkspaceID), `space ${s.ID}`).toBe(true);
    for (const t of tour.tasks) {
      expect(ws.has(t.WorkspaceID), `task ${t.ID}`).toBe(true);
      if (t.SpaceID !== null) expect(spaces.has(t.SpaceID), `task ${t.ID} space`).toBe(true);
    }
    for (const c of tour.chats) expect(tasks.has(c.TaskID), `chat ${c.ID}`).toBe(true);
    for (const r of tour.runs) {
      expect(tour.runLogs[r.ID], `run logs ${r.ID}`).toBeDefined();
      expect(chats.get(r.ChatID), `run ${r.ID} chat`).toBe(r.TaskID);
    }
    expect(chats.get(tour.live.chatId)).toBe(tour.live.taskId);
    for (const i of tour.inbox) {
      if (i.taskId !== undefined) expect(tasks.has(i.taskId), i.key).toBe(true);
      if (i.links.runId && i.links.runId !== tour.live.runId) expect(tour.runLogs[i.links.runId], i.key).toBeDefined();
    }
    expect(tour.diffs[tour.live.taskId]).toBeTruthy();
    expect(tour.terminals[tour.live.taskId]).toBeDefined();
  });

  it("S-02 holds no real paths, emails, token-shaped strings or owner names", () => {
    const text = everything();
    expect(text).not.toMatch(/[A-Za-z]:\\\\Users\\\\|\/Users\/|\/home\/(?!demo\/)/);
    expect(text).not.toMatch(/[\w.+-]+@[\w-]+\.[\w.]+/);
    expect(text).not.toMatch(/\b(sk-[A-Za-z0-9_-]{8,}|ghp_[A-Za-z0-9]{8,}|xox[bp]-|AKIA[0-9A-Z]{8,}|eyJ[A-Za-z0-9_-]{10,}\.)/);
    expect(text).not.toMatch(/ADMIN|lounguyen|long@scopedocs|scopedocs|spacingmind\/smind/i);
  });

  it("S-03 derives every timestamp from now: nothing in the future, nothing older than a month", () => {
    const iso = /\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z/g;
    // a usage window's reset is the one timestamp that is legitimately ahead of now
    const text = everything().replace(/"resetsAt":"[^"]*"/g, "");
    const stamps = [...text.matchAll(iso)].map((m) => Date.parse(m[0]));
    expect(stamps.length).toBeGreaterThan(20);
    for (const t of stamps) {
      expect(t).toBeLessThanOrEqual(NOW);
      expect(NOW - t).toBeLessThanOrEqual(31 * 24 * 3_600_000);
    }
    // a different now moves every timestamp with it
    const later = buildTour(NOW + 86_400_000);
    expect(Date.parse(later.tasks[0].UpdatedAt) - Date.parse(tour.tasks[0].UpdatedAt)).toBe(86_400_000);
  });

  it("S-04 the permission request fits the production permission types: kinds, a unified-diff body, a path", () => {
    expect(PERMISSION_REQUEST.options.map((o) => o.kind).sort()).toEqual(["allow_once", "reject_once"]);
    const d = PERMISSION_REQUEST.detail!;
    expect(d.kind).toBe("write");
    expect(d.path).toBe("internal/ratelimit/limiter.go");
    for (const line of d.diff!.split("\n")) expect(line).toMatch(/^( |\+|-|@@)/);
    expect(PERMISSION_REQUEST.requestId).toBe("demo-req-1");
    expect(tour.inbox.find((i) => i.kind === "permission")?.links.permissionRequestId).toBe(PERMISSION_REQUEST.requestId);
  });
});
