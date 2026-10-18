import { act } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";

// `isDesktop` is a module-load constant, so the platform is mocked per
// scenario and the dialog imported fresh (like desktop-update-dialog.test.tsx).
const PROVIDERS = {
  providers: [{ id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" }],
};
const AUTHORIZE_URL = "https://claude.ai/oauth/authorize?state=abc";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function startLogin(isDesktop: boolean) {
  vi.resetModules();
  const openExternal = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({ isDesktop, desktop: { openExternal } }));
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  const { AccountsDialog } = await import("@/components/accounts-dialog");

  const client = new FakeWsClient();
  render(<AccountsDialog client={client as never} open onOpenChange={() => {}} />);
  client.nth("account.list", 0).resolve([]);
  client.nth("provider.list").resolve(PROVIDERS);
  await flush();

  fireEvent.change(screen.getByLabelText("Label", { selector: "#account-oauth-label" }), { target: { value: "personal" } });
  fireEvent.click(screen.getByRole("button", { name: "Connect Claude Code" }));
  await waitFor(() => client.nth("account.oauthStart"));
  await act(async () => {
    client.emit("account.oauthStart", 0, "authorizeUrl", { url: AUTHORIZE_URL });
  });
  await flush();
  return { openExternal, open };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("AccountsDialog login URL (desktop webview swallows target=_blank)", () => {
  it("desktop: the authorize URL is opened in the system browser as soon as the daemon reports it", async () => {
    const { openExternal, open } = await startLogin(true);
    expect(openExternal).toHaveBeenCalledTimes(1);
    expect(openExternal).toHaveBeenCalledWith(AUTHORIZE_URL);
    expect(open).not.toHaveBeenCalled();
  });

  it("desktop: clicking 'open the login page' goes through the bridge, not a swallowed new-window navigation", async () => {
    const { openExternal } = await startLogin(true);
    openExternal.mockClear();
    const link = await screen.findByRole("link", { name: /open the login page/i });
    expect(fireEvent.click(link)).toBe(false); // default prevented
    expect(openExternal).toHaveBeenCalledWith(AUTHORIZE_URL);
  });

  it("web: nothing is auto-opened (popup blockers) and the link is a normal new-tab link", async () => {
    const { openExternal, open } = await startLogin(false);
    expect(openExternal).not.toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
    const link = await screen.findByRole("link", { name: /open the login page/i });
    expect(link).toHaveAttribute("href", AUTHORIZE_URL);
    expect(link).toHaveAttribute("target", "_blank");
    expect(fireEvent.click(link)).toBe(true);
  });
});
