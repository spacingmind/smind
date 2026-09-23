// client.ts kept Milestone 1's proof-of-life entry point working on top of
// Milestone 2's persistent RelayConnection (docs/plans/active/
// mobile-app-milestone-2.md Item 1): the admit -> E2EE handshake ->
// workspace.list sequence is now RelayConnection.connect + one call(),
// and the old one-shot "read exactly one response then close" behavior is
// the connect/call/close triple below. The returned workspaceListJSON is
// the same envelope-shaped text the old one-shot client returned
// ({id: "1", result: ...}), since Milestone 1's tests parse it as such.

import { PairingOffer } from './pairing';
import { RelayConnection } from './RelayConnection';

export interface ConnectResult {
  offer: PairingOffer;
  workspaceListJSON: string;
}

/**
 * connectAndFetchWorkspaceList runs the full chain a tap on "Connect"
 * needs: parse the pairing offer, admit to the relay, open the bridged
 * E2EE data session as the mobile role, complete the handshake (verifying
 * the daemon's public key against the one the offer carries), send one
 * workspace.list request, and return the response as raw JSON text.
 */
export async function connectAndFetchWorkspaceList(pairingUrl: string): Promise<ConnectResult> {
  const conn = await RelayConnection.connect(pairingUrl);
  try {
    const result = await conn.call('workspace.list');
    return { offer: conn.offer, workspaceListJSON: JSON.stringify({ id: '1', result }) };
  } finally {
    conn.close();
  }
}
