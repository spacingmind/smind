export interface RunStatusEvent {
  runId: string;
  chatId: number;
  status: string;
}

export const TERMINAL_STATUSES: readonly string[] = [
  'done',
  'error',
  'stopped',
  'interrupted',
];

export class RunFollower {
  private readonly chatId: number;
  private readonly onAdopt: (runId: string) => void;
  private readonly onLiveChange: (liveRunId: string | null) => void;
  private readonly known = new Set<string>();
  private live: string | null = null;

  constructor(
    chatId: number,
    onAdopt: (runId: string) => void,
    onLiveChange: (liveRunId: string | null) => void,
  ) {
    this.chatId = chatId;
    this.onAdopt = onAdopt;
    this.onLiveChange = onLiveChange;
  }

  get liveRunId(): string | null {
    return this.live;
  }

  markKnown(runId: string, status?: string): void {
    this.known.add(runId);
    if (status === 'running') {
      if (this.live !== runId) {
        this.live = runId;
        this.onLiveChange(runId);
      }
    } else if (status !== undefined && TERMINAL_STATUSES.includes(status)) {
      this.clearLive(runId);
    }
  }

  onRunStatus(e: RunStatusEvent): void {
    if (e.chatId !== this.chatId) {
      return;
    }
    if (e.status === 'running') {
      if (!this.known.has(e.runId)) {
        this.known.add(e.runId);
        this.live = e.runId;
        this.onAdopt(e.runId);
        this.onLiveChange(e.runId);
      } else if (this.live !== e.runId) {
        this.live = e.runId;
        this.onLiveChange(e.runId);
      }
    } else if (TERMINAL_STATUSES.includes(e.status)) {
      this.clearLive(e.runId);
    }
  }

  private clearLive(runId: string): void {
    if (this.live === runId) {
      this.live = null;
      this.onLiveChange(null);
    }
  }
}
