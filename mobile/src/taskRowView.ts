// taskRowView.ts maps a task status (+ attention flag) to the row's leading
// slot, status word and badge -- pure so every status is unit-tested.
//
// Slot rule: attention wins the slot with the UNREAD dot (it outranks every
// status except running, whose spinner stays -- a running task that needs
// approval still shows live progress). Otherwise: error -> danger dot,
// done/stopped/created -> idle dot, running -> spinner.

export interface TaskRowView {
  slot: 'spinner' | { dot: 'danger' | 'idle' | 'unread' };
  word: string;
  wordTone: 'default' | 'danger' | 'warning';
  badge: 'waiting' | null;
}

export function taskRowView(status: string, attention: boolean): TaskRowView {
  const word = statusWord(status);
  switch (status) {
    case 'running':
      // The spinner outranks the attention dot: live progress beats "needs you".
      return { slot: 'spinner', word, wordTone: 'warning', badge: attention ? 'waiting' : null };
    case 'error':
      return { slot: { dot: attention ? 'unread' : 'danger' }, word, wordTone: 'danger', badge: attention ? 'waiting' : null };
    default:
      return { slot: { dot: attention ? 'unread' : 'idle' }, word, wordTone: 'default', badge: attention ? 'waiting' : null };
  }
}

function statusWord(status: string): string {
  switch (status) {
    case 'created':
      return 'Created';
    case 'running':
      return 'Running';
    case 'done':
      return 'Done';
    case 'error':
      return 'Error';
    case 'stopped':
      return 'Stopped';
    default:
      return status.charAt(0).toUpperCase() + status.slice(1);
  }
}
