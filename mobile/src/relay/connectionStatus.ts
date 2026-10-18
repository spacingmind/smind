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

/** What the daemon-password prompt shows (ADR-0031 P13c), derived from the connection state. */
export interface PasswordPromptView {
  tone: StatusTone;
  title: string;
  detail: string;
  /** True while the daemon rate-limits this device: no input, a countdown. */
  locked: boolean;
}

/** The password prompt for state, or null when the state is not about the daemon password. `now` is epoch ms (the countdown). */
export function describePasswordState(state: ConnectionState, now: number = Date.now()): PasswordPromptView | null {
  if (state.kind === 'password') {
    return state.reason === 'incorrect'
      ? { tone: 'danger', title: 'Incorrect password', detail: 'The daemon refused that password. Check it and try again.', locked: false }
      : {
          tone: 'warning',
          title: 'Daemon password required',
          detail: 'This daemon is protected by a password. Enter it to connect; the app keeps it only until you close it.',
          locked: false,
        };
  }
  if (state.kind === 'locked') {
    const seconds = Math.max(0, Math.ceil((state.until - now) / 1000));
    return {
      tone: 'warning',
      title: 'Too many attempts',
      detail: `The daemon is limiting password attempts. Trying again in ${seconds} s, with the password you entered.`,
      locked: true,
    };
  }
  return null;
}
