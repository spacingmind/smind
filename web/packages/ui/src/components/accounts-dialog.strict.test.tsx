import { StrictMode, act } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConnectAccountPanel } from "@/components/accounts-dialog";
import { hostnameSource } from "@/hooks/use-login-remote";
import { FakeWsClient } from "@/test/fake-ws-client";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import type { ProviderInfo } from "@/lib/types";

// StrictMode simulates an unmount/re-mount; the login hook's cleanup aborts
// the first login, so autoStart must start a fresh one on the re-mount.
// Rendered with plain RTL: renderWithDaemon's harness swallows the strict
// double-invoke of effects.

const PROVIDERS = [
  { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
] as unknown as ProviderInfo[];

describe("ConnectAccountPanel under StrictMode", () => {
  it("autoStart starts exactly one live login under StrictMode", async () => {
    hostnameSource.get = () => "localhost";
    const ws = new FakeWsClient();
    const daemon = new FakeUsageDaemon([], ws);
    render(
      <StrictMode>
        <DaemonQueryProvider queryClient={createQueryClient()} client={daemon.client as never} events={null}>
          <ConnectAccountPanel
            client={daemon.client as never}
            providers={PROVIDERS}
            onConnected={vi.fn()}
            initialProviderId="anthropic"
            autoStart
            onClose={() => {}}
          />
        </DaemonQueryProvider>
      </StrictMode>,
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    await waitFor(() => {
      if (ws.calls.filter((c) => c.method === "account.oauthStart").length < 1) throw new Error("no start");
    });
    const starts = ws.calls.filter((c) => c.method === "account.oauthStart");
    expect(starts.length).toBeLessThanOrEqual(2);
    expect(starts[starts.length - 1].options?.signal?.aborted).not.toBe(true);
    expect(screen.getByRole("button", { name: /Connecting…/ })).toBeInTheDocument();
  });
});
