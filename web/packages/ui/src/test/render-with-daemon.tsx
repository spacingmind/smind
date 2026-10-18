import type { QueryClient } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";

import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { DaemonQueryProvider, useDaemonQueryClient } from "@/lib/query/daemon-query-provider";
import "@/test/query-scheduler";
import type { FakeUsageDaemon } from "@/test/usage-test-client";

/**
 * What the app root does (App.tsx): one QueryClient per connection (a new daemon swaps it) provided
 * with the connection's client and events, and the daemon's events bridged into the cache before any
 * listener sees them. Pass `queryClient` to keep one across unmounts.
 */
export function DaemonHarness({
  daemon,
  queryClient: given,
  onQueryClient,
  children,
}: {
  daemon: FakeUsageDaemon;
  queryClient?: QueryClient;
  onQueryClient?: (queryClient: QueryClient) => void;
  children: ReactNode;
}) {
  const own = useDaemonQueryClient();
  const queryClient = given ?? own;
  onQueryClient?.(queryClient);
  // The real hook: it subscribes, bridges events into the cache and invalidates on a same-daemon reconnect.
  useDaemonEvents(daemon.client, queryClient);
  return (
    <DaemonQueryProvider queryClient={queryClient} client={daemon.client} events={daemon.events}>
      {children}
    </DaemonQueryProvider>
  );
}

export interface DaemonRender extends RenderResult {
  /** The QueryClient of the latest connection. */
  queryClient: () => QueryClient;
  /** Re-renders under another daemon (a reconnect: new WsClient, new QueryClient) and/or another tree. */
  rerenderWith: (daemon: FakeUsageDaemon, ui?: ReactElement) => void;
}

/** Renders a tree inside a fresh `DaemonQueryProvider` fed by the fake daemon. */
export function renderWithDaemon(
  ui: ReactElement,
  daemon: FakeUsageDaemon,
  options: { queryClient?: QueryClient } = {},
): DaemonRender {
  let current: QueryClient | null = null;
  const wrap = (tree: ReactElement, d: FakeUsageDaemon) => (
    <DaemonHarness daemon={d} queryClient={options.queryClient} onQueryClient={(qc) => (current = qc)}>
      {tree}
    </DaemonHarness>
  );
  const view = render(wrap(ui, daemon));
  let tree = ui;
  return {
    ...view,
    queryClient: () => current!,
    rerenderWith: (next, nextUi) => {
      tree = nextUi ?? tree;
      view.rerender(wrap(tree, next));
    },
  };
}

/** Like testing-library's `renderHook`, inside the daemon harness. */
export function renderHookWithDaemon<T, P>(
  hook: (props: P) => T,
  daemon: FakeUsageDaemon,
  { initialProps, queryClient }: { initialProps: P; queryClient?: QueryClient },
) {
  const result = { current: undefined as T };
  let props = initialProps;
  let current = daemon;
  function Probe() {
    result.current = hook(props);
    return null;
  }
  const view = renderWithDaemon(<Probe />, daemon, { queryClient });
  return {
    result,
    queryClient: view.queryClient,
    unmount: view.unmount,
    rerender: (next: { props?: P; daemon?: FakeUsageDaemon } = {}) => {
      if (next.props !== undefined) props = next.props;
      current = next.daemon ?? current;
      view.rerenderWith(current);
    },
  };
}
