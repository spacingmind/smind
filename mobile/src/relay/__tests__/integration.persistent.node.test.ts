// integration.persistent.node.test.ts is Milestone 2's Item 1 end-to-end
// Test Scenario (docs/plans/active/mobile-app-milestone-2.md): one
// persistent RelayConnection against a real Go relay + daemon bridge,
// driving multiple sequential calls plus at least one real server-pushed
// eventNotification over that single connection, then verifying
// close()-then-call() rejects.
//
// It spawns its OWN harness instance rather than sharing
// integration.node.test.ts's because the milestone-1 bridge serves
// exactly one E2EE data session per workspace (bridge.go's fixed
// DefaultSessionID/DefaultDeviceID): a second, fresh-key mobile
// connection after a clean close leaves the daemon-side session unable
// to re-handshake (the relay route and its frame queues persist across
// the reconnect-grace window, so the bridge's retry cycle meets stale
// READY frames -- "expected hello, got 0x02"). The real app this
// milestone builds keeps ONE connection alive for the app's lifetime,
// which is exactly RelayConnection's contract; each connection getting
// a fresh harness here mirrors that one-connection-per-session reality.
//
// Run with: npm run test:integration (from mobile/), which requires `go`
// on PATH and the smind Go module to be present at ../ (this repo root).

/// <reference types="node" />
import { spawn, ChildProcessByStdio, execFileSync } from 'child_process';
import type { Readable } from 'stream';
import { mkdtempSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { RelayConnection } from '../RelayConnection';

const REPO_ROOT = join(__dirname, '..', '..', '..', '..');

let harnessBinary: string;
let harnessProcess: ChildProcessByStdio<null, Readable, Readable>;
let pairingUrl: string;
let tmpDir: string;
let wsDir: string;

let previousTLSReject: string | undefined;

beforeAll(async () => {
  // Same self-signed-cert caveat as integration.node.test.ts: this
  // override is test-only and does not touch the client code under test.
  previousTLSReject = process.env.NODE_TLS_REJECT_UNAUTHORIZED;
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

  tmpDir = mkdtempSync(join(tmpdir(), 'smind-relay-harness-bin-'));
  harnessBinary = join(tmpDir, 'relayharness');
  execFileSync('go', ['build', '-o', harnessBinary, './internal/relay/bridge/harness'], {
    cwd: REPO_ROOT,
    stdio: 'inherit',
  });

  // workspace.create requires an existing git repo (tasks are worktrees
  // branched off it), so seed a minimal one.
  wsDir = mkdtempSync(join(tmpdir(), 'smind-m2-it-ws-'));
  execFileSync('git', ['init', '-q'], { cwd: wsDir });
  execFileSync(
    'git',
    ['-c', 'user.name=smind-test', '-c', 'user.email=smind-test@example.com', '-C', wsDir, 'commit', '-q', '--allow-empty', '-m', 'init'],
  );

  pairingUrl = await new Promise<string>((resolve, reject) => {
    harnessProcess = spawn(harnessBinary, [], { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    const onData = (chunk: Buffer) => {
      stdout += chunk.toString('utf8');
      const match = stdout.match(/^READY (\S+)$/m);
      if (match) {
        harnessProcess.stdout.off('data', onData);
        resolve(match[1]);
      }
    };
    harnessProcess.stdout.on('data', onData);
    harnessProcess.stderr.on('data', (chunk: Buffer) => process.stderr.write(`[harness] ${chunk}`));
    harnessProcess.on('error', reject);
    harnessProcess.on('exit', (code) => reject(new Error(`harness exited early with code ${code}`)));
    setTimeout(() => reject(new Error('harness did not print READY in time')), 15_000);
  });
}, 60_000);

afterAll(() => {
  harnessProcess?.kill();
  if (tmpDir) rmSync(tmpDir, { recursive: true, force: true });
  if (wsDir) rmSync(wsDir, { recursive: true, force: true });
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = previousTLSReject;
});

describe('persistent connection against a real Go relay + daemon bridge', () => {
  it(
    'sustains multiple sequential calls and a pushed eventNotification over one connection',
    { timeout: 30_000 },
    async () => {
      const conn = await RelayConnection.connect(pairingUrl);
      try {
        // At least 3 sequential calls over the one admit+handshake (Item
        // 1's acceptance criterion), via the same mutating methods the
        // list screen will use.
        const created = (await conn.call('workspace.create', {
          path: wsDir,
          title: 'm2 integration',
        })) as { ID: number };
        expect(created.ID).toBeGreaterThan(0);

        const spaces = (await conn.call('space.list', { workspaceId: created.ID })) as unknown[];
        expect(spaces).toEqual([]);

        const task = (await conn.call('task.create', {
          workspaceId: created.ID,
          spaceId: null,
          title: 'first task',
        })) as { ID: number };
        expect(task.ID).toBeGreaterThan(0);

        // Subscribe, then mutate again: task.created must arrive as a
        // real server-pushed eventNotification over this same connection.
        const pushed: Array<{ topic: string; payload: unknown }> = [];
        const unsubscribe = conn.subscribe(['task.created', 'task.updated'], (ev) =>
          pushed.push({ topic: ev.topic, payload: ev.payload }),
        );

        const task2 = (await conn.call('task.create', {
          workspaceId: created.ID,
          spaceId: null,
          title: 'second task',
        })) as { ID: number };
        expect(task2.ID).toBeGreaterThan(task.ID);

        await vi.waitFor(() => expect(pushed.length).toBeGreaterThanOrEqual(1), { timeout: 10_000 });
        expect(pushed[0].topic).toBe('task.created');
        unsubscribe();

        // Calls keep working after subscribe/unsubscribe traffic on the
        // same connection...
        const tasks = (await conn.call('task.list', { workspaceId: created.ID })) as Array<{ ID: number }>;
        expect(tasks.map((t) => t.ID).sort((a, b) => a - b)).toEqual(
          [task.ID, task2.ID].sort((a, b) => a - b),
        );

        // ...an error response rejects just that call...
        await expect(conn.call('task.get', { id: 999999 })).rejects.toThrow();

        // ...and a clean close ends the session for good.
        conn.close();
        await expect(conn.call('workspace.list')).rejects.toThrow('relay: connection is closed');
      } finally {
        conn.close();
      }
    },
  );
});
