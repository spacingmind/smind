// integration.node.test.ts is the plan's manual-verification Test
// Scenario, automated: it spawns a real relay + a real daemon bridge
// (internal/relay/bridge/devharness: a real relay and a real multi-device
// daemon hub with a device registry, a throwaway Go binary built just for
// these tests) and drives this package's real, unmodified client code
// against it over a real network path -- proving the TypeScript grpc-web
// + E2EE client actually interoperates with the real Go server, not just
// that each side's own unit tests pass against hand-rolled doubles.
//
// Milestone 1 proved one workspace.list round trip per connection.
// Milestone 2 (docs/plans/active/mobile-app-milestone-2.md) extends this
// to the persistent-connection contract: many sequential calls over one
// admit+handshake (workspace.create -> space.list -> task.create ->
// task.list), and a real server-pushed eventNotification delivered over
// the same connection after events.subscribe (the task.created topic
// fired by workspace.create's own mutation).
//
// This runs under plain Node (not Hermes/React Native): Node 20+'s global
// fetch/WebSocket are used by mobile/src/relay/grpcweb.ts exactly as React
// Native's are, so this exercises the real client code, just not the real
// JS engine a device would use. It is opt-in (see mobile/package.json's
// "test:integration" script) because it needs the Go toolchain to build
// the harness binary, unlike the rest of this package's unit tests.
//
// P13b: the daemon bridge now refuses clients without a device identity
// (relay.legacyDevices is off by default), so the connection below identifies
// itself with relay.device_hello and is admitted by the harness's pairing
// window (one new device).
//
// Run with: npm run test:integration (from mobile/), which requires `go`
// on PATH and the smind Go module to be present at ../ (this repo root).

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { connectAndFetchWorkspaceList } from '../client';
import { DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;

beforeAll(async () => {
  // The harness's relay uses a fresh self-signed cert; see requireSelfSignedHarnessTrust.
  requireSelfSignedHarnessTrust();
  harness = await DevHarness.start(1);
}, 90_000);

afterAll(() => {
  harness?.stop();
});

describe(`end-to-end against a real Go relay + daemon bridge`, () => {
  it('completes admission, E2EE handshake, device_hello and one workspace.list round trip', async () => {
    const result = await connectAndFetchWorkspaceList(harness.pairingUrl, testConnectOptions(1));
    expect(result.workspaceListJSON).toBeTruthy();
    const parsed = JSON.parse(result.workspaceListJSON);
    expect(parsed.id).toBe('1');
    expect(parsed.result).toEqual([]);
    // The daemon registered the device under its key-derived id.
    const rows = await harness.devices();
    expect(rows).toHaveLength(1);
    expect(rows[0].state).toBe('approved');
    expect(rows[0].label).toBe('node test device 1');
  });
});
