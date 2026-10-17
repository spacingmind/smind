// connectionStatus.ts turns a RelayConnection's state into what the person
// holding the phone should read (kept free of React Native so the copy is
// unit-tested): why the app is not showing their workspace and what to do
// about it. The pending copy names the exact daemon command, since approval
// happens on the computer running smind (ADR-0031 D9).

import { ConnectionState } from './RelayConnection';

export type StatusTone = 'info' | 'warning' | 'danger';

export interface StatusView {
  tone: StatusTone;
  title: string;
  detail: string;
  /** A command to run on the computer running smind, shown monospaced and selectable. */
  command?: string;
}

/** The status to show for state, or null when there is nothing to say (connecting, connected, closed). */
export function describeState(state: ConnectionState): StatusView | null {
  switch (state.kind) {
    case 'pending':
      return {
        tone: 'warning',
        title: 'Waiting for approval',
        detail: `“${state.label}” is not approved on the daemon yet. On the computer running smind, run:`,
        command: `smind relay devices approve ${state.deviceId}`,
      };
    case 'revoked':
      return {
        tone: 'danger',
        title: 'This device was revoked',
        detail: 'The daemon no longer accepts this device. Reset its identity to ask for approval again as a new device.',
      };
    case 'failed':
      if (state.versionMismatch) {
        return { tone: 'danger', title: 'Daemon outdated or on a different version', detail: state.message, command: 'smind update' };
      }
      return { tone: 'danger', title: 'Couldn’t connect', detail: state.message };
    case 'reconnecting':
      return { tone: 'info', title: 'Reconnecting…', detail: state.reason };
    default:
      return null;
  }
}

/** The pairing screen's note on certificates: a phone does not pin a self-signed relay certificate (ADR-0031 P13b). */
export const CERTIFICATE_HELP =
  'The relay needs a certificate your phone already trusts (for example from Let’s Encrypt). A phone does not accept a self-signed relay certificate.';
