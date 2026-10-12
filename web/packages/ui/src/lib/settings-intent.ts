/**
 * One-slot handoff from the composer's model picker to Settings -> Agents:
 * the picker records what the user asked for ("create a profile from this
 * model" / "edit that profile"), navigates to Settings, and the Agents
 * section takes the intent on mount -- or via subscribe when it is already open.
 */
export interface ProfileSeed {
  name: string;
  provider: string;
  model: string;
  permissionMode?: string;
  autoAccept?: boolean;
  thinkingLevel?: string;
}

export type SettingsIntent = { kind: "newProfile"; seed: ProfileSeed } | { kind: "editProfile"; id: number };

let pending: SettingsIntent | null = null;
const listeners = new Set<() => void>();

export function setSettingsIntent(intent: SettingsIntent): void {
  pending = intent;
  for (const listener of [...listeners]) listener();
}

/** Returns the pending intent and clears it. */
export function takeSettingsIntent(): SettingsIntent | null {
  const intent = pending;
  pending = null;
  return intent;
}

/** `listener` runs on every set; it should call takeSettingsIntent itself. */
export function subscribeSettingsIntent(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
