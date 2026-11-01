import type { InboxItem } from "@/lib/inbox-types";
import type { Chat, RunLogEvent, RunLogsResult, RunStatusValue, RunSummary, Task } from "@/lib/types";

import { REDUCED_MOTION_DELAY_MS, type DemoClock } from "./clock";
import { gitOf } from "./git";
import { DemoTerminal } from "./terminal";
import type { DemoScenario, Script, Step } from "./types";

/** What a run.attach subscriber receives: an event, or the end of the run. */
type RunListener = (item: { event: RunLogEvent } | { end: { stopReason: string } }) => void;

/** A run the scripted daemon is playing (or has finished): its event log, status, and who is attached. */
export class LiveRun {
  readonly events: RunLogEvent[] = [];
  status: RunStatusValue = "running";
  stopReason = "";
  /** The permission request the script is paused on, if any. */
  pending: { requestId: string; resolve: (optionId: string) => void; optionIds: string[] } | null = null;
  private readonly listeners = new Set<RunListener>();
  private cancels: Array<() => void> = [];
  private stopped = false;

  constructor(
    readonly id: string,
    readonly taskId: number,
    readonly chatId: number,
    readonly provider: string,
    readonly prompt: string,
    readonly startedAt: string,
  ) {}

  append(event: RunLogEvent): void {
    this.events.push(event);
    for (const l of this.listeners) l({ event });
  }

  /** Replays the log so far, then follows live. Returns the detach function. */
  attach(listener: RunListener): () => void {
    for (const event of this.events) listener({ event });
    if (this.status !== "running") {
      listener({ end: { stopReason: this.stopReason } });
      return () => {};
    }
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  finish(status: RunStatusValue, stopReason: string): void {
    if (this.status !== "running") return;
    this.status = status;
    this.stopReason = stopReason;
    this.pending = null;
    const ls = [...this.listeners];
    this.listeners.clear();
    for (const l of ls) l({ end: { stopReason } });
  }

  trackCancel(cancel: () => void): void {
    this.cancels.push(cancel);
  }

  /** Aborts the script (run.stop / task.cancel): nothing is emitted afterwards. */
  halt(): void {
    this.stopped = true;
    for (const c of this.cancels) c();
    this.cancels = [];
  }

  get isHalted(): boolean {
    return this.stopped;
  }
}

type NotifyFn = (topic: string, payload: unknown) => void;

/**
 * The page-load state of the scripted daemon (ADR-0051 section 1): a mutable copy of the scenario, the runs being
 * played, and the notification fan-out to whichever socket is connected. It outlives a socket, so a reconnect keeps it.
 */
export class DemoSession {
  workspaces;
  spaces;
  tasks: Task[];
  chats: Chat[];
  runSummaries: RunSummary[];
  inbox: InboxItem[];
  readonly runs = new Map<string, LiveRun>();
  readonly terminals = new Map<string, DemoTerminal>();
  /** Task ids whose live permission was allowed (adds the extra diff file). */
  readonly allowedTasks = new Set<number>();
  promptCount = 0;
  private nextRun = 1;
  private nextTask: number;
  private nextChat: number;
  private readonly notifyListeners = new Set<NotifyFn>();

  constructor(
    readonly scenario: DemoScenario,
    readonly clock: DemoClock,
    readonly reducedMotion: boolean,
  ) {
    // The scenario holds a function (reply), so only its data lists are cloned: the session mutates its own copies.
    this.workspaces = structuredClone(scenario.workspaces);
    this.spaces = structuredClone(scenario.spaces);
    this.tasks = structuredClone(scenario.tasks);
    this.chats = structuredClone(scenario.chats);
    this.runSummaries = structuredClone(scenario.runs);
    this.inbox = structuredClone(scenario.inbox);
    this.nextTask = Math.max(0, ...this.tasks.map((t) => t.ID)) + 1;
    this.nextChat = Math.max(0, ...this.chats.map((c) => c.ID)) + 1;
    this.startLive();
  }

  /** ISO timestamp `ms` before the scenario's now. */
  ago(ms: number): string {
    return new Date(this.scenario.now - ms).toISOString();
  }

  nowIso(): string {
    return new Date(this.clock.now()).toISOString();
  }

  onNotify(fn: NotifyFn): () => void {
    this.notifyListeners.add(fn);
    return () => this.notifyListeners.delete(fn);
  }

  notify(topic: string, payload: unknown): void {
    for (const fn of this.notifyListeners) fn(topic, payload);
  }

  /** inbox.seen / snooze / resolve: update the in-memory rows and push the notification the daemon would. */
  markInbox(keys: string[], state: InboxItem["state"], until?: string): Record<string, never> {
    const set = new Set(keys);
    this.inbox = this.inbox.map((it) => (set.has(it.key) ? { ...it, state, ...(state === "snoozed" && until ? { snoozedUntil: until } : {}) } : it));
    this.notify("inbox.changed", {});
    return {};
  }

  allocTaskId(): number {
    return this.nextTask++;
  }

  allocChatId(): number {
    return this.nextChat++;
  }

  /** Starts the run that is in progress on first paint. */
  private startLive(): void {
    const { live } = this.scenario;
    const run = new LiveRun(live.runId, live.taskId, live.chatId, live.provider, live.prompt, this.nowIso());
    this.runs.set(run.id, run);
    this.runSummaries.push(this.summaryOf(run));
    this.play(run, live.script);
  }

  /** run.start: the visitor's prompt is echoed and the canned reply plays. */
  startReply(taskId: number, chatId: number, provider: string, prompt: string): LiveRun {
    const id = `demo-run-${this.nextRun++}`;
    const run = new LiveRun(id, taskId, chatId, provider, prompt, this.nowIso());
    this.runs.set(id, run);
    this.runSummaries.push(this.summaryOf(run));
    this.notify("run.status", { runId: id, taskId, chatId, status: "running" });
    this.play(run, this.scenario.reply(prompt, this.promptCount++));
    return run;
  }

  summaryOf(run: LiveRun): RunSummary {
    return {
      ID: run.id,
      TaskID: run.taskId,
      ChatID: run.chatId,
      Provider: run.provider,
      Prompt: run.prompt,
      Status: run.status,
      StartedAt: run.startedAt,
      FinishedAt: run.status === "running" ? null : this.nowIso(),
      StopReason: run.stopReason,
      Err: "",
      PermissionMode: "default",
      AutoAccept: false,
      ThinkingLevel: "",
      Model: "",
    };
  }

  /** run.list / run.logs read the live status, not the snapshot taken at start. */
  runList(filter: { chatId?: number; taskId?: number }): RunSummary[] {
    return this.runSummaries
      .map((s) => {
        const live = this.runs.get(s.ID);
        return live ? { ...this.summaryOf(live), StartedAt: s.StartedAt } : s;
      })
      .filter((s) => (filter.chatId === undefined || s.ChatID === filter.chatId) && (filter.taskId === undefined || s.TaskID === filter.taskId));
  }

  logsOf(runId: string): RunLogsResult | null {
    const live = this.runs.get(runId);
    if (live) return { runId, status: live.status, stopReason: live.stopReason || undefined, events: [...live.events] };
    return this.scenario.runLogs[runId] ?? null;
  }

  /** Answers the permission the run is paused on. Returns false for an unknown run/request or an option it never offered. */
  respond(runId: string, requestId: string, optionId: string): boolean {
    const run = this.runs.get(runId);
    const p = run?.pending;
    if (!run || !p || p.requestId !== requestId || !p.optionIds.includes(optionId)) return false;
    p.resolve(optionId);
    return true;
  }

  stopRun(runId: string): boolean {
    const run = this.runs.get(runId);
    if (!run || run.status !== "running") return false;
    run.halt();
    run.finish("stopped", "cancelled");
    this.notify("run.status", { runId, taskId: run.taskId, chatId: run.chatId, status: "stopped", stopReason: "cancelled" });
    this.afterRun(run);
    return true;
  }

  /** Plays a script on a run: events append to its log (and reach attached subscribers) at their scripted times. */
  private play(run: LiveRun, script: Script): void {
    this.playSteps(run, script.steps, () => {
      if (run.status === "running") {
        run.finish("done", "end_turn");
        this.notify("run.status", { runId: run.id, taskId: run.taskId, chatId: run.chatId, status: "done", stopReason: "end_turn" });
        this.afterRun(run);
      }
    });
  }

  private playSteps(run: LiveRun, steps: Step[], onEnd: () => void): void {
    const delay = (ms: number) => (this.reducedMotion ? REDUCED_MOTION_DELAY_MS : ms);
    let i = 0;
    let prevAt = 0;
    const next = (): void => {
      if (run.isHalted) return;
      const step = steps[i++];
      if (!step) {
        onEnd();
        return;
      }
      const wait = Math.max(0, step.at - prevAt);
      prevAt = step.at;
      run.trackCancel(
        this.clock.after(delay(wait), () => {
          if (run.isHalted) return;
          if ("event" in step) {
            run.append(step.event);
            next();
          } else if ("notify" in step) {
            this.notify(step.notify.topic, step.notify.payload);
            next();
          } else {
            run.append({ type: "permission_request", ...step.ask });
            this.notify("permission.pending", { runId: run.id, taskId: run.taskId, chatId: run.chatId, requestId: step.ask.requestId });
            this.notify("inbox.changed", {});
            run.pending = {
              requestId: step.ask.requestId,
              optionIds: step.ask.options.map((o) => o.id),
              resolve: (optionId) => {
                run.pending = null;
                const kind = step.ask.options.find((o) => o.id === optionId)?.kind ?? "";
                run.append({ type: "permission_resolved", requestId: step.ask.requestId, optionId, reason: "human" });
                this.onPermissionAnswered(run, step.ask.requestId, kind.startsWith("allow"));
                this.playSteps(run, kind.startsWith("allow") ? step.allow : step.deny, onEnd);
              },
            };
          }
        }),
      );
    };
    next();
  }

  private onPermissionAnswered(run: LiveRun, requestId: string, allowed: boolean): void {
    const key = `perm:${requestId}`;
    this.inbox = this.inbox.map((it) => (it.key === key ? { ...it, state: "resolved" } : it));
    if (allowed) this.allowedTasks.add(run.taskId);
    this.notify("inbox.changed", {});
    const task = this.tasks.find((t) => t.ID === run.taskId);
    if (allowed && task) this.notify("task.git", { taskId: task.ID, git: gitOf(this, task) });
  }

  private afterRun(run: LiveRun): void {
    this.notify("task.status", { taskId: run.taskId, status: "active" });
    this.notify("inbox.changed", {});
  }
}
