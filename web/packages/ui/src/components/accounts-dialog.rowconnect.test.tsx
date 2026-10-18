import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import type { Account } from "@/lib/types";

// The ConnectAccountPanel's row-level/embedded modes (PR-A1): autoStart
// starts a login once on mount, relogin targets an existing account id
// (with a legacy-account confirmation), and onClose closes the embedded
// form -- reusing the harness of accounts-dialog.connect.test.tsx.

const PROVIDERS = {
  providers: [
    { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
    { id: "glm", label: "GLM", kind: "cli" },
    { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
    { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai" },
  ],
};

interface PlatformMock {
  isDesktop: boolean;
  connection?: { id: string; kind: "local" | "url" } | null;
}

function account(id: number, provider: string, label: string, extra: Record<string, unknown> = {}) {
  return { id, provider, label, credentialType: "oauth", createdAt: "", updatedAt: "", ...extra };
}

async function setup(
  platform: PlatformMock = { isDesktop: false },
  accounts: unknown[] = [],
  props: { initialProviderId?: string; autoStart?: boolean; relogin?: Account; onClose?: () => void } = {},
) {
  vi.resetModules();
  const openExternal = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({
    isDesktop: platform.isDesktop,
    desktop: {
      openExternal,
      getCurrentConnection: () =>
        platform.connection ? Promise.resolve({ label: "c", baseUrl: "", ...platform.connection }) : Promise.reject(new Error("none")),
    },
  }));
  cleanup();
  const { ConnectAccountPanel } = await import("@/components/accounts-dialog");
  const { renderWithDaemon } = await import("@/test/render-with-daemon");
  const { FakeUsageDaemon } = await import("@/test/usage-test-client");
  const { useLoginLocationStore } = await import("@/stores/login-location-store");
  const { hostnameSource } = await import("@/hooks/use-login-remote");
  hostnameSource.get = () => "localhost";

  useLoginLocationStore.getState().setOverride("c1", null);
  useLoginLocationStore.getState().setOverride("web", null);

  const client = new FakeWsClient();
  const daemon = new FakeUsageDaemon([], client);
  daemon.accounts = accounts as never;
  const { createQueryClient } = await import("@/lib/query/query-client");
  const queryClient = createQueryClient();
  const onConnected = vi.fn();
  const panel = (
    <ConnectAccountPanel
      client={daemon.client as never}
      providers={PROVIDERS.providers as never}
      onConnected={onConnected}

      {...props}
    />
  );
  renderWithDaemon(panel, daemon, { queryClient });
  if (client.calls.some((c) => c.method === "account.list")) {
    await act(async () => {
      client.nth("account.list").resolve(accounts);
    });
  }
  await flush();
  return { client, onConnected };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("ConnectAccountPanel row connect", () => {
  it("autoStart starts the login once with the default label", async () => {
    const { client } = await setup(undefined, [], {
      initialProviderId: "anthropic",
      autoStart: true,
      onClose: () => {},
    });
    const call = await waitFor(() => client.nth("account.oauthStart"));
    expect(call.params).toMatchObject({ provider: "anthropic", label: "account-1", method: "browser" });
    await flush();
    // Exactly once: no StrictMode-style double start.
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(1);
  });

  it("autoStart does not start for an api key provider and shows the form", async () => {
    const { client } = await setup(undefined, [], {
      initialProviderId: "kimi",
      autoStart: true,
      onClose: () => {},
    });
    await flush();
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(0);
    expect(screen.getByLabelText("Credential")).toBeInTheDocument();
  });

  it("relogin starts with the account id and the account label", async () => {
    const legacy = account(7, "anthropic", "work", { hasIdentity: true });
    const { client } = await setup(undefined, [legacy], {
      relogin: legacy,
      autoStart: true,
      onClose: () => {},
    });
    const call = await waitFor(() => client.nth("account.oauthStart"));
    expect(call.params).toMatchObject({ provider: "anthropic", label: "work", method: "browser", accountId: 7 });
  });

  it("relogin of a legacy account asks for confirmation before starting", async () => {
    const legacy = account(9, "anthropic", "old-key", { hasIdentity: false });
    const { client } = await setup(undefined, [legacy], {
      relogin: legacy,
      autoStart: true,
      onClose: () => {},
    });
    await flush();
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(0);
    const notice = screen.getByTestId("relogin-confirm");
    expect(notice).toHaveTextContent(
      "This replaces the credential of ‘old-key’ with whichever account you sign in to.",
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("relogin-confirm-continue"));
    });
    const call = await waitFor(() => client.nth("account.oauthStart"));
    expect(call.params).toMatchObject({ provider: "anthropic", label: "old-key", accountId: 9 });
  });

  it("relogin hides the provider and label fields and the api key method", async () => {
    const legacy = account(7, "anthropic", "work", { hasIdentity: true });
    await setup(undefined, [legacy], { relogin: legacy, onClose: () => {} });
    expect(screen.queryByLabelText("Provider")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Label", { selector: "#account-oauth-label" })).not.toBeInTheDocument();
    expect(screen.getByText("Signing in again as ‘work’")).toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: "API key" })).not.toBeInTheDocument();
  });

  it("onClose runs when the form is closed and after a finished login is dismissed", async () => {
    const onClose = vi.fn();
    const { client } = await setup(undefined, [], {
      initialProviderId: "anthropic",
      autoStart: true,
      onClose,
    });
    expect(screen.getByTestId("connect-close")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("connect-close"));
    expect(onClose).toHaveBeenCalledTimes(1);

    // A finished login's dismiss also closes the form.
    const call = await waitFor(() => client.nth("account.oauthStart"));
    void call;
    await act(async () => {
      client.nth("account.oauthStart").resolve({
        id: 3,
        provider: "anthropic",
        label: "account-1",
        credentialType: "oauth",
        createdAt: "",
        updatedAt: "",
        created: true,
      });
    });
    await flush();
    expect(screen.getByTestId("login-done-notice")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("login-notice-dismiss"));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("relogin panel is headed Sign in again", async () => {
    const legacy = account(7, "anthropic", "work", { hasIdentity: true });
    await setup(undefined, [legacy], { relogin: legacy, onClose: () => {} });
    expect(screen.getByText("Sign in again")).toBeInTheDocument();
  });
});
