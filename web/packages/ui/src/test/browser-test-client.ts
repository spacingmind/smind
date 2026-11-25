import type { BrowserSettingsView } from "@/lib/browser-settings-types";
import { FakeUsageDaemon } from "@/test/usage-test-client";

/** What a scripted `browser.*` handler returns: a result, or `undefined` to fall through to the base fake. */
export type BrowserHandler = (method: string, params: unknown) => unknown;

/**
 * The usage fake with `browser.*` RPCs answered by a handler, every call recorded in `calls`
 * (so tests assert exactly which RPCs ran and with what params). Throw an `RpcError` to fail a call.
 */
export function browserDaemon(handler: BrowserHandler): FakeUsageDaemon {
  const daemon = new FakeUsageDaemon([]);
  const base = daemon.client.call.bind(daemon.client);
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown): Promise<unknown> => {
    if (!method.startsWith("browser.")) return base(method, params);
    daemon.calls.push({ method, params });
    return handler(method, params);
  };
  return daemon;
}

/** A settings view as the daemon answers it for a workspace that never stored anything (off, empty, installed). */
export function browserView(partial: Partial<BrowserSettingsView> = {}): BrowserSettingsView {
  return {
    workspaceId: 1,
    enabled: false,
    allowHosts: [],
    profileMode: "ephemeral",
    killSwitch: false,
    available: true,
    chromeInstalled: true,
    note: "A host name may only resolve to public addresses; to allow an intranet or local-network host, add its IP address instead. Loopback (localhost, 127.0.0.1, ::1) is always allowed and needs no entry.",
    providers: [
      { id: "claude-native", label: "Claude Code", reach: "yes", images: "yes" },
      { id: "glm", label: "GLM", reach: "conditional", why: "an ACP agent takes the browser only if it advertises MCP support; smind cannot tell in advance; GLM cannot read images, so only the text tools are useful", images: "no" },
      { id: "codex-native", label: "Codex", reach: "no", why: "Codex takes no per-run MCP servers", images: "yes" },
    ],
    sessionLive: false,
    ...partial,
  };
}
