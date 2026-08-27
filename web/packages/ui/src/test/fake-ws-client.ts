import type { CallOptions, EventFunc, WsClientLike } from "@/lib/ws-client";

/** One call recorded by FakeWsClient, kept pending until the test resolves/rejects it (or emits events on it). */
export interface RecordedCall {
  method: string;
  params: unknown;
  onEvent?: EventFunc;
  options?: CallOptions;
  resolve: (value: unknown) => void;
  reject: (err: unknown) => void;
}

/**
 * A fake implementing WsClientLike's public surface (call/callStream) --
 * not a fake WebSocket underneath WsClient, which ws-client.test.ts's
 * FakeSocket already covers one level down (see that file's doc comment).
 * Every call is recorded and left pending: tests drive resolution/events
 * by hand via `nth`/`emit`, so component tests can exercise exact
 * ordering and race scenarios (backfill before live chunks, a stale fetch
 * resolving after the selection moved on, abort-not-stop on unmount)
 * deterministically, without a real network round trip.
 */
export class FakeWsClient implements WsClientLike {
  calls: RecordedCall[] = [];

  call<TResult = unknown>(method: string, params?: unknown, options?: CallOptions): Promise<TResult> {
    return this.callStream<TResult>(method, params, undefined, options);
  }

  callStream<TResult = unknown>(
    method: string,
    params?: unknown,
    onEvent?: EventFunc,
    options?: CallOptions,
  ): Promise<TResult> {
    return new Promise<TResult>((resolve, reject) => {
      this.calls.push({
        method,
        params,
        onEvent,
        options,
        resolve: resolve as (value: unknown) => void,
        reject,
      });
    });
  }

  /** Returns the nth (0-indexed, in call order) recorded call for method. Throws if it hasn't happened yet. */
  nth(method: string, index = 0): RecordedCall {
    const matches = this.calls.filter((c) => c.method === method);
    const call = matches[index];
    if (!call) throw new Error(`no call #${index} to ${method} recorded (have ${matches.length})`);
    return call;
  }

  /** Fires a server-pushed event on the nth recorded call for method. */
  emit(method: string, index: number, event: string, params: unknown): void {
    this.nth(method, index).onEvent?.(event, params);
  }
}
