import { useQueryClient } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { DaemonQueryProvider, useDaemonQueryClient } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { WsClient } from "@/lib/ws-client";

const fakeClient = () => ({}) as unknown as WsClient;

function Host({
  client,
  identity,
  onClient,
}: {
  client: WsClient | null;
  identity: string;
  onClient: (qc: ReturnType<typeof useQueryClient>) => void;
}) {
  const queryClient = useDaemonQueryClient(client, identity);
  return (
    <DaemonQueryProvider queryClient={queryClient} client={client} events={null}>
      <Probe onClient={onClient} />
    </DaemonQueryProvider>
  );
}

function Probe({ onClient }: { onClient: (qc: ReturnType<typeof useQueryClient>) => void }) {
  onClient(useQueryClient());
  return null;
}

describe("daemon query client", () => {
  it("a reconnect to the same daemon keeps the cache and invalidates it", () => {
    const seen: ReturnType<typeof useQueryClient>[] = [];
    const first = fakeClient();
    const { rerender } = render(<Host client={first} identity="ws://daemon-a/ws" onClient={(qc) => seen.push(qc)} />);
    const queryClient = seen.at(-1)!;
    queryClient.setQueryData(queryKeys.accountList(), [{ id: 1 }]);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    // Same client re-rendered: nothing happens.
    rerender(<Host client={first} identity="ws://daemon-a/ws" onClient={(qc) => seen.push(qc)} />);
    expect(invalidate).not.toHaveBeenCalled();

    // New WsClient, same daemon: same QueryClient, data still there (no loading flash), one invalidation.
    act(() => rerender(<Host client={fakeClient()} identity="ws://daemon-a/ws" onClient={(qc) => seen.push(qc)} />));
    expect(seen.at(-1)).toBe(queryClient);
    expect(queryClient.getQueryData(queryKeys.accountList())).toEqual([{ id: 1 }]);
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("the first connect does not invalidate", () => {
    const seen: ReturnType<typeof useQueryClient>[] = [];
    const { rerender } = render(<Host client={null} identity="ws://daemon-a/ws" onClient={(qc) => seen.push(qc)} />);
    const invalidate = vi.spyOn(seen.at(-1)!, "invalidateQueries");
    act(() => rerender(<Host client={fakeClient()} identity="ws://daemon-a/ws" onClient={(qc) => seen.push(qc)} />));
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("query client is per connection and cleared on switch", () => {
    const seen: ReturnType<typeof useQueryClient>[] = [];
    const { rerender } = render(<Host client={fakeClient()} identity="ws://daemon-a/ws" onClient={(qc) => seen.push(qc)} />);
    const first = seen.at(-1)!;
    first.setQueryData(queryKeys.accountList(), [{ id: 1 }]);

    // Another daemon: a fresh QueryClient that never sees the old data; the old one is cleared.
    act(() => rerender(<Host client={fakeClient()} identity="ws://daemon-b/ws" onClient={(qc) => seen.push(qc)} />));
    const second = seen.at(-1)!;
    expect(second).not.toBe(first);
    expect(second.getQueryData(queryKeys.accountList())).toBeUndefined();
    expect(first.getQueryData(queryKeys.accountList())).toBeUndefined();
  });
});
