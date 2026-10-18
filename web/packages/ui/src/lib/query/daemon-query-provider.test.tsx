import { useQueryClient } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DaemonQueryProvider, useDaemonQueryClient } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { WsClient } from "@/lib/ws-client";

const fakeClient = () => ({}) as unknown as WsClient;

function Host({ client, onClient }: { client: WsClient | null; onClient: (qc: ReturnType<typeof useQueryClient>) => void }) {
  const queryClient = useDaemonQueryClient(client);
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
  it("query client is per connection and cleared on switch", () => {
    const seen: ReturnType<typeof useQueryClient>[] = [];
    const first = fakeClient();
    const { rerender } = render(<Host client={first} onClient={(qc) => seen.push(qc)} />);
    const firstClient = seen.at(-1)!;
    firstClient.setQueryData(queryKeys.accountList(), [{ id: 1 }]);

    // Same connection re-rendered: same QueryClient, data kept.
    rerender(<Host client={first} onClient={(qc) => seen.push(qc)} />);
    expect(seen.at(-1)).toBe(firstClient);
    expect(firstClient.getQueryData(queryKeys.accountList())).toEqual([{ id: 1 }]);

    // A new WsClient (reconnect or another daemon): a fresh QueryClient that never sees the old data.
    act(() => rerender(<Host client={fakeClient()} onClient={(qc) => seen.push(qc)} />));
    const secondClient = seen.at(-1)!;
    expect(secondClient).not.toBe(firstClient);
    expect(secondClient.getQueryData(queryKeys.accountList())).toBeUndefined();
    expect(firstClient.getQueryData(queryKeys.accountList())).toBeUndefined();
  });
});
