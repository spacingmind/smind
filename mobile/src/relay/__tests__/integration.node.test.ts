// integration.node.test.ts is the plan's manual-verification Test
// Scenario, automated: it spawns a real relay + a real daemon bridge
// (internal/relay/bridge/harness, a throwaway Go binary built just for
// this test) and drives this package's real, unmodified client code
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
// Run with: npm run test:integration (from mobile/), which requires `go`
// on PATH and the smind Go module to be present at ../ (this repo root).

/// <reference types="node" />
import { spawn, ChildProcessByStdio, execFileSync } from 'child_process';
import type { Readable } from 'stream';
import { mkdtempSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { connectAndFetchWorkspaceList } from '../client';

const REPO_ROOT = join(__dirname, '..', '..', '..', '..');

let harnessBinary: string;
let harnessProcess: ChildProcessByStdio<null, Readable, Readable>;
let pairingUrl: string;
let tmpDir: string;

let previousTLSReject: string | undefined;

beforeAll(async () => {
  // The harness's relay uses a fresh self-signed cert (server.LoadOrCreateCert),
  // same as any self-hosted `smind relay`. Real mobile pairing targets the
  // Cloudflare-fronted, publicly-trusted-cert case (see the plan's
  // Decisions: self-signed pinning from a mobile client is explicitly out
  // of this milestone's scope) -- there is no real CA to trust here, so
  // this test-only override disables Node's TLS verification purely for
  // this harness's ad hoc local cert. It does not touch grpcweb.ts/
  // client.ts, which stay exactly what a real device would run.
  previousTLSReject = process.env.NODE_TLS_REJECT_UNAUTHORIZED;
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

  tmpDir = mkdtempSync(join(tmpdir(), 'smind-relay-harness-bin-'));
  harnessBinary = join(tmpDir, 'relayharness');
  execFileSync('go', ['build', '-o', harnessBinary, './internal/relay/bridge/harness'], {
    cwd: REPO_ROOT,
    stdio: 'inherit',
  });

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
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = previousTLSReject;
});

describe('end-to-end against a real Go relay + daemon bridge', () => {
  it('completes admission, E2EE handshake, and one workspace.list round trip', async () => {
    const result = await connectAndFetchWorkspaceList(pairingUrl);
    expect(result.workspaceListJSON).toBeTruthy();
    const parsed = JSON.parse(result.workspaceListJSON);
    expect(parsed.id).toBe('1');
    expect(parsed.result).toEqual([]);
  });

});

