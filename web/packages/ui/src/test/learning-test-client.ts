import { FakeUsageDaemon } from "@/test/usage-test-client";
import type { LearningStatus } from "@/lib/learning-types";

/** What a scripted `learning.*` handler returns: a result, or `undefined` to fall through to the base fake. */
export type LearningHandler = (method: string, params: unknown) => unknown;

/**
 * The usage fake with `learning.*` RPCs answered by a handler, every call recorded in `calls`
 * (so tests assert call counts and params). Throw an `RpcError` from the handler to fail a call.
 */
export function learningDaemon(handler: LearningHandler): FakeUsageDaemon {
  const daemon = new FakeUsageDaemon([]);
  const base = daemon.client.call.bind(daemon.client);
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown): Promise<unknown> => {
    if (!method.startsWith("learning.")) return base(method, params);
    daemon.calls.push({ method, params });
    return handler(method, params);
  };
  return daemon;
}

export function learningStatus(partial: Partial<LearningStatus> = {}): LearningStatus {
  return { enabled: true, workspaces: [], budget: null, ...partial };
}
