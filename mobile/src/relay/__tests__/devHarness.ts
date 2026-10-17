// devHarness.ts builds and drives internal/relay/bridge/devharness (a real
// Go relay, a real multi-device daemon hub with a device registry, and Go
// "desktop" peers) for the *.node.test.ts integration tests. Commands are one
// line on the harness's stdin, answered by one JSON line on stdout.

/// <reference types="node" />
import { spawn, ChildProcessWithoutNullStreams, execFileSync } from 'child_process';
import { mkdtempSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { ConnectOptions } from '../RelayConnection';
import { identityFromSeed } from '../deviceIdentity';

const REPO_ROOT = join(__dirname, '..', '..', '..', '..');

export interface HarnessReply {
  cmd: string;
  ok: boolean;
  error?: string;
  [key: string]: unknown;
}

export interface DeviceRow {
  id: string;
  label: string;
  state: 'approved' | 'pending' | 'revoked';
  connected: boolean;
}

export class DevHarness {
  private lines: string[] = [];
  private waiters: Array<(line: string) => void> = [];
  private stdoutBuf = '';

  /** The pairing URL the harness printed (set by start()). */
  pairingUrl = '';

  private constructor(
    private readonly proc: ChildProcessWithoutNullStreams,
    private readonly tmpDir: string,
  ) {
    this.attachReader();
  }

  /**
   * Builds the harness binary and starts it; windowMax > 0 opens a pairing
   * window for that many new devices. outdatedDaemon runs a daemon from before
   * device_hello v2 instead of the hub: it handshakes and then never sends a
   * device challenge.
   */
  static async start(windowMax = 0, outdatedDaemon = false): Promise<DevHarness> {
    const tmpDir = mkdtempSync(join(tmpdir(), 'smind-devharness-bin-'));
    const binary = join(tmpDir, 'devharness');
    execFileSync('go', ['build', '-o', binary, './internal/relay/bridge/devharness'], { cwd: REPO_ROOT, stdio: 'inherit' });
    const args = [...(windowMax > 0 ? ['-window-max', String(windowMax)] : []), ...(outdatedDaemon ? ['-outdated-daemon'] : [])];
    const proc = spawn(binary, args, { stdio: ['pipe', 'pipe', 'pipe'] });
    proc.stderr.on('data', (c: Buffer) => {
      if (process.env.DEVHARNESS_LOG) process.stderr.write(`[devharness] ${c}`);
    });
    const harness = new DevHarness(proc, tmpDir);
    const exited = new Promise<never>((_, reject) => proc.on('exit', (code) => reject(new Error(`devharness exited early with code ${code}`))));
    try {
      for (;;) {
        const line = await Promise.race([harness.nextLine(20_000), exited]);
        if (line.startsWith('READY ')) {
          harness.pairingUrl = line.slice('READY '.length).trim();
          return harness;
        }
      }
    } catch (e) {
      harness.stop();
      throw e;
    }
  }

  private attachReader(): void {
    this.proc.stdout.on('data', (chunk: Buffer) => {
      this.stdoutBuf += chunk.toString('utf8');
      let i: number;
      while ((i = this.stdoutBuf.indexOf('\n')) >= 0) {
        const line = this.stdoutBuf.slice(0, i);
        this.stdoutBuf = this.stdoutBuf.slice(i + 1);
        const w = this.waiters.shift();
        if (w) w(line);
        else this.lines.push(line);
      }
    });
  }

  private nextLine(timeoutMs: number): Promise<string> {
    const queued = this.lines.shift();
    if (queued !== undefined) return Promise.resolve(queued);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('devharness: no reply in time')), timeoutMs);
      this.waiters.push((l) => {
        clearTimeout(timer);
        resolve(l);
      });
    });
  }

  /** Sends one command and returns its JSON reply, skipping the NATIVE banner line. */
  async cmd(line: string, timeoutMs = 30_000): Promise<HarnessReply> {
    this.proc.stdin.write(line + '\n');
    for (;;) {
      const raw = await this.nextLine(timeoutMs);
      if (!raw.startsWith('{')) continue;
      return JSON.parse(raw) as HarnessReply;
    }
  }

  async mustCmd(line: string, timeoutMs?: number): Promise<HarnessReply> {
    const r = await this.cmd(line, timeoutMs);
    if (!r.ok) throw new Error(`devharness ${line}: ${r.error}`);
    return r;
  }

  async devices(): Promise<DeviceRow[]> {
    return ((await this.mustCmd('list')).devices ?? []) as DeviceRow[];
  }

  stop(): void {
    this.proc.kill('SIGKILL');
    rmSync(this.tmpDir, { recursive: true, force: true });
  }
}

/**
 * Options for a test connection: a deterministic identity per seed byte,
 * quick approval polling and the short handshake windows tests want.
 */
export function testConnectOptions(seedByte: number, over: Partial<ConnectOptions> = {}): ConnectOptions {
  return {
    identity: identityFromSeed(new Uint8Array(32).fill(seedByte)),
    label: `node test device ${seedByte}`,
    reconnect: { pendingPollMs: 300, baseMs: 100, maxMs: 1_000, resumeProbeMs: 8_000, handshakeMs: 15_000 },
    ...over,
  };
}
