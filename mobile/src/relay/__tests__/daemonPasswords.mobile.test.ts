import { readFileSync } from 'fs';
import { join } from 'path';
import { describe, expect, it } from 'vitest';

// Modules that moved to the shared package (mobile keeps one-line shims at the old paths).
const moved = new Set(['daemonPasswords', 'RelayConnection', 'deviceKeyStore']);

describe('a password is memory only', () => {
  // Code only: the comments explain what is NOT done and name the APIs by design.
  const resolve = (rel: string) => {
    const m = /^src\/relay\/(\w+)\.ts$/.exec(rel);
    return m && moved.has(m[1]) ? join(__dirname, '..', '..', '..', '..', 'web', 'packages', 'relay-client', 'src', `${m[1]}.ts`) : join(__dirname, '..', '..', '..', rel);
  };
  const code = (rel: string) =>
    readFileSync(resolve(rel), 'utf8')
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/^\s*\/\/.*$/gm, '');
  const read = code;

  it('the files that handle it import no persistence and call no logging or analytics API', () => {
    for (const rel of ['src/relay/daemonPasswords.ts', 'src/screens/DaemonPasswordPrompt.tsx', 'src/relay/RelayConnection.ts', 'src/screens/PairingScreen.tsx', 'src/connection/ConnectionProvider.tsx', 'app/connection.tsx']) {
      const src = read(rel);
      expect(src, rel).not.toMatch(/expo-secure-store|async-storage|AsyncStorage|localStorage|sessionStorage|expo-file-system|expo-sqlite|MMKV/);
      expect(src, rel).not.toMatch(/console\.(log|info|warn|error|debug)/);
      expect(src, rel).not.toMatch(/analytics|crashlytics|sentry|segment|mixpanel/i);
    }
  });

  it('the secure-store modules never mention the password', () => {
    for (const rel of ['src/relay/deviceKeyStore.ts', 'src/relay/platformKeyStore.ts']) expect(read(rel).toLowerCase(), rel).not.toContain('password');
  });
});
