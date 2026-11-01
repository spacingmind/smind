// pluginViewRelay.test.ts: the panel reads against a REAL RelayConnection over a handshaked
// E2EE v2 channel (relayHarness.ts's FakeDaemon against the in-memory pipe): the daemon's typed
// view errors (a view over the byte cap, a panel above this client's ui level) reject as
// DaemonError with the daemon's own code and message, and the transports themselves carried
// nothing but ciphertext -- no method or panel name appears in the clear on the wire.

import { describe, expect, it } from 'vitest';
import { Channel } from '../relay/e2ee';
import { daemonKeysFrom, FakeV2Daemon, frameKinds, memPipe, MemTransport } from '../relay/__tests__/fakeV2Daemon';
import { DaemonError, RelayConnection } from '../relay/RelayConnection';
import { FakeDaemon, makeRelayHarness } from '../relayHarness';
import { fetchPluginPanels, fetchPluginView } from '../plugins/api';

describe('plugin.view over the relay', () => {
  it('frameCapRefused: a view over the byte cap rejects as view_invalid, message unchanged, no partial view', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const p = fetchPluginView(conn, 'acme', 'overview');
    const [req] = await daemon.waitForRequests(1);
    expect(req.method).toBe('plugin.view');
    expect(req.params).toEqual({ pluginId: 'acme', panelId: 'overview', ui: 1 });
    daemon.channel.send(
      new TextEncoder().encode(JSON.stringify({ id: req.id, error: { message: 'plugin.view: view exceeds 262144 bytes at root.children[3]', code: 'view_invalid' } })),
    );
    // The call rejects, so no truncated document ever reaches the caller.
    const err = await p.then(
      () => null,
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(DaemonError);
    expect((err as DaemonError).code).toBe('view_invalid');
    expect((err as DaemonError).message).toBe('plugin.view: view exceeds 262144 bytes at root.children[3]');
  });

  it('a panel above this client ui level rejects as ui_level_too_low', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const p = fetchPluginView(conn, 'acme', 'charts');
    const [req] = await daemon.waitForRequests(1);
    daemon.channel.send(new TextEncoder().encode(JSON.stringify({ id: req.id, error: { message: 'plugin.view: panel needs ui 2', code: 'ui_level_too_low' } })));
    const err = await p.then(
      () => null,
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(DaemonError);
    expect((err as DaemonError).code).toBe('ui_level_too_low');
  });
});

/** The harness of relayHarness.ts, but keeping both MemTransport ends so a test can inspect what each side put on the wire. */
async function tappedPair(): Promise<{ conn: RelayConnection; daemon: FakeDaemon; client: MemTransport; server: MemTransport }> {
  const [server, client] = memPipe();
  const keys = daemonKeysFrom();
  const daemon = new FakeDaemon(new FakeV2Daemon(server, keys));
  const daemonReady = daemon.start();
  const channel = new Channel(client);
  await channel.handshake(keys.publicKey);
  await daemonReady;
  const conn = new RelayConnection(
    { daemonId: '', publicKey: keys.publicKey, relay: '', relayFingerprint: '', secret: new Uint8Array(), workspaceId: '' },
    { close: () => {} },
    channel,
  );
  return { conn, daemon, client, server };
}

/** Whether bytes contains needle as a byte subsequence. */
function contains(bytes: Uint8Array, needle: Uint8Array): boolean {
  for (let i = 0; i + needle.length <= bytes.length; i++) {
    let j = 0;
    while (j < needle.length && bytes[i + j] === needle[j]) j++;
    if (j === needle.length) return true;
  }
  return false;
}

describe('E2EE-v2 smoke: panels and view travel as ciphertext only', () => {
  it('the daemon answers both reads, while neither transport ever saw a method or panel name in the clear', async () => {
    const { conn, daemon, client, server } = await tappedPair();
    const view = { type: 'panel', children: [{ type: 'text', text: 'Acme overview' }] };

    const panels = fetchPluginPanels(conn);
    const reqs = await daemon.waitForRequests(1);
    expect(reqs[0].method).toBe('plugin.panels');
    expect(reqs[0].params).toEqual({ ui: 1 });
    daemon.reply(reqs[0].id!, { panels: [{ plugin: 'acme', id: 'overview', title: 'Overview', where: 'task', ui: 1 }], sidebar: [] });
    const got = await panels;
    expect(got.panels[0]).toMatchObject({ plugin: 'acme', id: 'overview', where: 'task', ui: 1 });

    const viewP = fetchPluginView(conn, 'acme', 'overview');
    const both = await daemon.waitForRequests(2);
    expect(both[1].method).toBe('plugin.view');
    expect(both[1].params).toEqual({ pluginId: 'acme', panelId: 'overview', ui: 1 });
    daemon.reply(both[1].id!, { pluginId: 'acme', panelId: 'overview', view });
    // The view document arrives as data (a JSON round-trip, so deep equality, not identity).
    await expect(viewP).resolves.toEqual(view);

    // Not vacuous: each side really sent the handshake plus the two request/response data frames.
    for (const end of [client, server]) {
      const kinds = frameKinds(end.sent);
      expect(kinds.filter((k) => k === 'data').length).toBeGreaterThanOrEqual(2);
    }
    const needles = ['plugin.view', 'plugin.panels', 'overview'].map((s) => new TextEncoder().encode(s));
    for (const frame of [...client.sent, ...server.sent]) {
      for (const needle of needles) expect(contains(frame, needle), `${frame[4]} frame leaked plaintext`).toBe(false);
    }
  });
});
