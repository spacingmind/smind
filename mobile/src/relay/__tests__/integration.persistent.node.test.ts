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
// P13b: identifies itself with device_hello (admitted by the harness's pairing
// window), as the daemon refuses clients without a device identity.
//
// Run with: npm run test:integration (from mobile/), which requires `go`
// on PATH and the smind Go module to be present at ../ (this repo root).

/// <reference types="node" />
import { execFileSync } from 'child_process';
import { mkdtempSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { RelayConnection } from '../RelayConnection';
import { DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;
let wsDir: string;

beforeAll(async () => {
  // The harness's relay uses a fresh self-signed cert; see requireSelfSignedHarnessTrust.
  requireSelfSignedHarnessTrust();

  // workspace.create requires an existing git repo (tasks are worktrees
  // branched off it), so seed a minimal one.
  wsDir = mkdtempSync(join(tmpdir(), 'smind-m2-it-ws-'));
  execFileSync('git', ['init', '-q'], { cwd: wsDir });
  execFileSync(
    'git',
    ['-c', 'user.name=smind-test', '-c', 'user.email=smind-test@example.com', '-C', wsDir, 'commit', '-q', '--allow-empty', '-m', 'init'],
  );

  harness = await DevHarness.start(1);
}, 90_000);

afterAll(() => {
  harness?.stop();
  if (wsDir) rmSync(wsDir, { recursive: true, force: true });
});

describe(`persistent connection against a real Go relay + daemon bridge`, () => {
  it(
    'sustains multiple sequential calls and a pushed eventNotification over one connection',
    { timeout: 30_000 },
    async () => {
      const conn = await RelayConnection.connect(harness.pairingUrl, testConnectOptions(2));
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
