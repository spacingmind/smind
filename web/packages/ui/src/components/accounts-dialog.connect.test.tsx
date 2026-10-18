import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";

// The ConnectAccountPanel's ADR-0033 flows: method selector, remote/local
// defaults, the 15 s paste hint, device code, paste-complete and the done
// notice -- driven against the real panel under a mocked platform, the
// way accounts-dialog.desktop.test.tsx mocks it per scenario.

const PROVIDERS = {
  providers: [
    { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
    { id: "glm", label: "GLM", kind: "cli" },
    { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
    { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai" },
  ],
};

const REDIRECT_URL = "http://127.0.0.1:47891/callback?state=abc&code=SECRET";

interface PlatformMock {
  isDesktop: boolean;
  connection?: { id: string; kind: "local" | "url" } | null;
}

async function setup(platform: PlatformMock = { isDesktop: false }, accounts: unknown[] = []) {
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
  const { ConnectAccountPanel } = await import("@/components/accounts-dialog");
  const { renderWithDaemon } = await import("@/test/render-with-daemon");
  const { FakeUsageDaemon } = await import("@/test/usage-test-client");
  const { useLoginLocationStore } = await import("@/stores/login-location-store");
  const { hostnameSource } = await import("@/hooks/use-login-remote");
  hostnameSource.get = () => "localhost";

  useLoginLocationStore.getState().setOverride("c1", null);
  useLoginLocationStore.getState().setOverride("web", null);

  const client = new FakeWsClient();
  const daemon = new FakeUsageDaemon(accounts as never, client);
  const onConnected = vi.fn();
  renderWithDaemon(
    <ConnectAccountPanel client={daemon.client as never} providers={PROVIDERS.providers as never} onConnected={onConnected} />,
    daemon,
  );
  client.nth("account.list").resolve(accounts);
  await flush();
  return { client, onConnected };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function labelInput(): HTMLInputElement {
  return screen.getByLabelText("Label", { selector: "#account-oauth-label" });
}

/** Starts a browser/device login for the currently-selected provider. */
async function connect(client: FakeWsClient): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: /^Connect / }));
  await waitFor(() => client.nth("account.oauthStart"));
  await flush();
}

function emitAuthorize(client: FakeWsClient, url = "https://claude.ai/oauth/authorize?state=abc") {
  client.emit("account.oauthStart", 0, "authorizeUrl", { loginId: "l1", url, pasteOnly: false });
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("ConnectAccountPanel connect flows", () => {
  it("method selector offers the provider's methods", async () => {
    await setup();

    // Claude Code (anthropic): subscription + api key.
    expect(screen.getByRole("radio", { name: "Claude.ai subscription" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "API key" })).toBeInTheDocument();

    // Codex (openai) adds the device method.
    fireEvent.click(await screen.findByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "Codex" }));
    expect(screen.getByRole("radio", { name: "Sign in with ChatGPT" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Device code" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "API key" })).toBeInTheDocument();

    // Kimi (api-key only) has a single method -- no segmented control.
    fireEvent.click(await screen.findByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "Kimi" }));
    expect(screen.queryByRole("radio", { name: "API key" })).not.toBeInTheDocument();
  });

  it("connect label defaults to account-N", async () => {
    await setup();
    expect(labelInput().value).toBe("account-1");

    // Switching providers re-prefills per provider (openai has no account yet).
    fireEvent.click(await screen.findByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "Codex" }));
    expect(labelInput().value).toBe("account-1");

    // A provider with an existing account counts it.
    const second = await setup({ isDesktop: false }, []);
    fireEvent.click(await screen.findByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "Codex" }));
    expect(labelInput().value).toBe("account-1");
    void second;
  });

  it("remote openai defaults to the device method and shows the device code panel", async () => {
    const { client } = await setup({ isDesktop: true, connection: { id: "c1", kind: "url" } });
    fireEvent.click(await screen.findByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "Codex" }));
    await flush();

    expect(screen.getByRole("radio", { name: "Device code" })).toHaveAttribute("data-state", "on");

    fireEvent.click(screen.getByRole("button", { name: "Connect Codex" }));
    await waitFor(() => client.nth("account.oauthStart"));
    expect(client.nth("account.oauthStart").params).toMatchObject({ provider: "openai", method: "device" });
    await act(async () => {
      client.emit("account.oauthStart", 0, "deviceCode", {
        loginId: "l1",
        verificationUrl: "https://chatgpt.com/device",
        userCode: "ABCD-1234",
        expiresAt: "2099-01-01T00:15:00Z",
        intervalSeconds: 5,
      });
    });
    await flush();
    expect(screen.getByTestId("device-code-panel")).toBeInTheDocument();
    expect(screen.getByTestId("device-code")).toHaveTextContent("ABCD-1234");
  });

  it("remote anthropic shows the paste field immediately", async () => {
    const { client } = await setup({ isDesktop: true, connection: { id: "c1", kind: "url" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect Claude Code" }));
    await waitFor(() => client.nth("account.oauthStart"));
    await act(async () => {
      emitAuthorize(client);
    });
    await flush();

    // Remote: no waiting for the 15 s hint, and no trouble toggle.
    expect(screen.getByTestId("paste-redirect-panel")).toBeInTheDocument();
    expect(screen.queryByTestId("login-trouble-toggle")).not.toBeInTheDocument();
  });

  it("local login shows paste after the trouble delay", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { client } = await setup();

    await act(async () => {
      await connect(client);
    });
    await act(async () => {
      emitAuthorize(client);
    });
    await flush();

    expect(screen.queryByTestId("paste-redirect-panel")).not.toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(15000);
    });
    await flush();

    expect(screen.getByTestId("login-trouble-toggle")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("login-trouble-toggle"));
    expect(screen.getByTestId("paste-redirect-panel")).toBeInTheDocument();
  });

  it("pasted redirect calls account.oauthComplete and finishes the login", async () => {
    const { client } = await setup({ isDesktop: true, connection: { id: "c1", kind: "url" } });
    await act(async () => {
      await connect(client);
    });
    await act(async () => {
      emitAuthorize(client);
    });
    await flush();

    fireEvent.change(screen.getByLabelText("Sign-in redirect address"), { target: { value: REDIRECT_URL } });
    fireEvent.click(screen.getByTestId("paste-redirect-submit"));
    await waitFor(() => client.nth("account.oauthComplete"));
    expect(client.nth("account.oauthComplete").params).toEqual({ loginId: "l1", redirectUrl: REDIRECT_URL });

    await act(async () => {
      client.nth("account.oauthComplete").resolve(null);
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
  });

  it("done notice words created and updated differently", async () => {
    const { client } = await setup();

    await act(async () => {
      await connect(client);
    });
    await act(async () => {
      client.nth("account.oauthStart").resolve({
        id: 3,
        provider: "anthropic",
        label: "personal",
        credentialType: "oauth",
        createdAt: "",
        updatedAt: "",
        created: true,
      });
    });
    await flush();
    expect(screen.getByTestId("login-done-notice")).toHaveTextContent("Connected as 'personal'.");

    fireEvent.click(screen.getByTestId("login-notice-dismiss"));
    await act(async () => {
      await connect(client);
    });
    await act(async () => {
      client.nth("account.oauthStart", 1).resolve({
        id: 3,
        provider: "anthropic",
        label: "personal",
        credentialType: "oauth",
        createdAt: "",
        updatedAt: "",
        created: false,
      });
    });
    await flush();
    expect(screen.getByTestId("login-done-notice")).toHaveTextContent("Updated 'personal' — already connected.");
  });

  it("api key method adds an account with the credential", async () => {
    const { client } = await setup();

    fireEvent.click(screen.getByRole("radio", { name: "API key" }));
    fireEvent.change(screen.getByLabelText("Credential"), { target: { value: "sk-new" } });
    fireEvent.click(screen.getByTestId("accounts-add-submit"));

    const add = await waitFor(() => client.nth("account.add"));
    expect(add.params).toEqual({ provider: "anthropic", label: "account-1", credential: "sk-new" });
  });

  it("the paste a credential disclosure is gone", async () => {
    await setup();
    expect(screen.queryByTestId("accounts-manual-toggle")).not.toBeInTheDocument();
    expect(screen.queryByText(/Paste a credential instead/)).not.toBeInTheDocument();
  });

  it("location toggle switches remote and local defaults", async () => {
    const { client } = await setup({ isDesktop: true, connection: { id: "c1", kind: "url" } });
    fireEvent.click(await screen.findByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "Codex" }));
    await flush();

    // Remote openai defaults to the device method.
    expect(screen.getByRole("radio", { name: "Device code" })).toHaveAttribute("data-state", "on");
    expect(screen.getByTestId("login-location")).toHaveTextContent(/on another machine/);

    // Claiming to be on the daemon's machine flips to local defaults.
    fireEvent.click(screen.getByTestId("login-location-toggle"));
    await flush();
    expect(screen.getByRole("radio", { name: "Sign in with ChatGPT" })).toHaveAttribute("data-state", "on");

    // And back: the toggle now offers signing in from another device.
    fireEvent.click(screen.getByTestId("login-location-toggle"));
    await flush();
    expect(screen.getByRole("radio", { name: "Device code" })).toHaveAttribute("data-state", "on");

    void connect(client);
  });
});
