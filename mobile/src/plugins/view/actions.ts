// actions.ts parses plugin view actions -- the only interactive surface an
// untrusted document gets. The openUrl host is never trusted from the
// document: it is recomputed locally from the url, so a plugin cannot name a
// host it is not opening. Pure TS, no react-native import.
import type { ViewAction } from './types';

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === 'object' && x !== null && !Array.isArray(x);
}

function nonEmptyString(x: unknown): x is string {
  return typeof x === 'string' && x !== '';
}

// React Native's URL is incomplete, so the parse is manual: an http(s) scheme
// (case-insensitive), then the authority runs to the first / ? or #. userinfo
// (@), control characters and an empty authority are rejected outright.
export function openUrlHost(url: string): string | null {
  const m = /^https?:\/\/([^/?#]*)/i.exec(url);
  if (!m) return null;
  const authority = m[1];
  if (authority === '') return null;
  for (let i = 0; i < authority.length; i++) {
    const c = authority.charCodeAt(i);
    if (authority[i] === '@' || c < 0x20 || c === 0x7f) return null;
  }
  return authority.toLowerCase();
}

export function parseAction(x: unknown): ViewAction | null {
  if (!isRecord(x)) return null;
  switch (x.kind) {
    case 'rpc': {
      if (!nonEmptyString(x.rpc)) return null;
      const action: ViewAction = { kind: 'rpc', rpc: x.rpc };
      if (isRecord(x.params)) action.params = x.params;
      return action;
    }
    case 'prompt':
      return nonEmptyString(x.text) ? { kind: 'prompt', text: x.text } : null;
    case 'command':
      return nonEmptyString(x.name) ? { kind: 'command', name: x.name } : null;
    case 'openPanel': {
      if (!nonEmptyString(x.panel)) return null;
      const action: ViewAction = { kind: 'openPanel', panel: x.panel };
      if (isRecord(x.context)) action.context = x.context;
      return action;
    }
    case 'openUrl': {
      if (!nonEmptyString(x.url)) return null;
      const host = openUrlHost(x.url);
      return host === null ? null : { kind: 'openUrl', url: x.url, host };
    }
    default:
      return null;
  }
}

/** The plugin.rpc request an rpc action becomes; pluginId is always the caller's, never the document's. */
export function rpcRequest(
  pluginId: string,
  action: { rpc: string; params?: Record<string, unknown> },
  values?: Record<string, string | boolean>,
): { pluginId: string; rpc: string; params: Record<string, unknown> } {
  const params: Record<string, unknown> = { ...action.params };
  if (values !== undefined) params.values = values;
  return { pluginId, rpc: action.rpc, params };
}
