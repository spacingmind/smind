/// <reference types="node" />
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { DevHarness, requireSelfSignedHarnessTrust } from '@smind/relay-client/src/__tests__/devHarness';
import { WsClient, type DaemonNotification } from '../../../ui/src/lib/ws-client';
import { pairAndApprove, type Paired } from './harnessSupport';

let harness: DevHarness;
let wsDir: string;
let paired: Paired | undefined;

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  wsDir = mkdtempSync(join(tmpdir(), 'smind-web-it-ws-'));
  execFileSync('git', ['init', '-q'], { cwd: wsDir });
  execFileSync('git', ['-c', 'user.name=smind-test', '-c', 'user.email=smind-test@example.com', '-C', wsDir, 'commit', '-q', '--allow-empty', '-m', 'init']);
  harness = await DevHarness.start(0);
}, 90_000);

afterAll(() => {
  paired?.controller.close();
  harness?.stop();
  if (wsDir) rmSync(wsDir, { recursive: true, force: true });
});

describe('web relay client against the real Go relay and hub', () => {
  it('integration_pair_call_and_event', { timeout: 60_000 }, async () => {
    paired = await pairAndApprove(harness, 'web it');
    const client = new WsClient(await paired.controller.socket());

    const ws = (await client.call('workspace.create', { path: wsDir, title: 'web it' })) as { ID: number };
    expect(ws.ID).toBeGreaterThan(0);
    expect(await client.call('space.list', { workspaceId: ws.ID })).toEqual([]);

    const got: DaemonNotification[] = [];
    client.onNotification((n) => got.push(n));
    await client.call('events.subscribe', { topics: ['task.created'] });
    await client.call('task.create', { workspaceId: ws.ID, spaceId: null, title: 'pushed' });
    await vi.waitFor(() => expect(got.length).toBeGreaterThanOrEqual(1), { timeout: 10_000 });
    expect(got[0].topic).toBe('task.created');

    const rows = await harness.devices();
    expect(rows.find((r) => r.id === paired!.deviceId)).toMatchObject({ state: 'approved', label: 'web it' });
  });
});
