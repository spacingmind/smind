import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/github-section";
import type { ForgeStatus } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

// A fake string, never a credential.
const FAKE_TOKEN = "ghp_FAKE0000000000000000000000000000TEST";

function Harness(_: { client: FakeWsClient }) {
  const section = listSettingsSections().find((s) => s.id === "github")!;
  return <>{section.render({ client: null })}</>;
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

function status(over: Partial<ForgeStatus> = {}): ForgeStatus {
  return {
    provider: "github",
    canSetToken: true,
    cli: { installed: true, version: "2.101.0" },
    hosts: [{ host: "github.com", authenticated: true, login: "octo", scopes: ["repo", "read:org"], protocol: "https", source: "keyring" }],
    ...over,
  };
}

async function mount(s: ForgeStatus) {
  const client = new FakeWsClient();
  const view = render(<Harness client={client} />);
  client.nth("forge.status", 0).resolve(s);
  await flush();
  return { client, view };
}

function type(testId: string, value: string) {
  fireEvent.change(screen.getByTestId(testId), { target: { value } });
}

afterEach(() => vi.restoreAllMocks());

describe("Settings → GitHub", () => {
  it("is registered as a settings section", () => {
    expect(listSettingsSections().map((s) => s.id)).toContain("github");
  });

  it("shows each status state", async () => {
    const signedIn = await mount(status());
    expect(screen.getByTestId("github-host-github.com")).toHaveTextContent("octo");
    expect(screen.getByTestId("github-host-github.com")).toHaveTextContent("repo, read:org");
    expect(screen.getByTestId("github-host-github.com")).toHaveTextContent("https");
    expect(screen.getByTestId("github-host-github.com")).toHaveTextContent("system keychain");
    expect(screen.getByTestId("github-cli-version")).toHaveTextContent("gh 2.101.0");
    signedIn.view.unmount();

    const signedOut = await mount(status({ hosts: [{ host: "github.com", authenticated: false, login: "", scopes: [], protocol: "", source: "none" }] }));
    expect(screen.getByTestId("github-signed-out")).toHaveTextContent("Not signed in");
    expect(screen.getByTestId("github-token-input")).toBeEnabled();
    signedOut.view.unmount();

    const noCli = await mount(status({ cli: { installed: false, version: "" }, hosts: [] }));
    expect(screen.getByTestId("github-cli-missing")).toHaveTextContent("isn't installed on the daemon host");
    expect(screen.getByTestId("github-token-input")).toBeDisabled();
    noCli.view.unmount();
  });

  it("follows forge.status events", async () => {
    const { client } = await mount(status({ hosts: [] }));
    expect(screen.getByTestId("github-signed-out")).toBeInTheDocument();
    await act(async () => client.pushNotification("forge.status", { status: status() }));
    expect(screen.getByTestId("github-host-github.com")).toHaveTextContent("octo");
    await act(async () => client.pushNotification("forge.status", { status: status({ hosts: [] }) }));
    expect(screen.queryByTestId("github-host-github.com")).not.toBeInTheDocument();
  });

  it("keeps this connection's canSetToken when a forge.status event arrives (the event cannot carry it)", async () => {
    const { client } = await mount(status({ canSetToken: true, hosts: [] }));
    expect(screen.getByTestId("github-token-input")).toBeEnabled();
    const { canSetToken: _omitted, ...eventStatus } = status();
    void _omitted;
    await act(async () => client.pushNotification("forge.status", { status: eventStatus }));
    expect(screen.getByTestId("github-host-github.com")).toHaveTextContent("octo");
    expect(screen.getByTestId("github-token-input")).toBeEnabled();
    expect(screen.queryByTestId("github-insecure")).not.toBeInTheDocument();
  });

  it("disables the field on an insecure channel", async () => {
    await mount(status({ canSetToken: false }));
    expect(screen.getByTestId("github-token-input")).toBeDisabled();
    expect(screen.getByTestId("github-insecure")).toHaveTextContent(
      "This connection isn't encrypted, so smind won't send a token over it. Use the relay, an ssh tunnel or TLS.",
    );
    expect(screen.getByTestId("github-token-submit")).toBeDisabled();
  });

  it("clears the token on submit and unmount", async () => {
    const { client, view } = await mount(status({ hosts: [] }));
    const input = screen.getByTestId("github-token-input") as HTMLInputElement;
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveAttribute("autocomplete", "off");
    type("github-token-input", FAKE_TOKEN);
    expect(input.value).toBe(FAKE_TOKEN);
    fireEvent.click(screen.getByTestId("github-token-submit"));
    await flush();
    // Cleared before the answer arrives.
    expect(input.value).toBe("");
    expect(client.nth("forge.setToken", 0).params).toEqual({ token: FAKE_TOKEN });
    client.nth("forge.setToken", 0).resolve({ status: status() });
    await flush();
    expect(screen.getByTestId("github-signed-in-note")).toBeInTheDocument();
    expect((screen.getByTestId("github-token-input") as HTMLInputElement).value).toBe("");
    expect(document.body.innerHTML).not.toContain(FAKE_TOKEN);

    // A half-typed token does not survive an unmount either.
    type("github-token-input", FAKE_TOKEN);
    view.unmount();
    const again = await mount(status({ hosts: [] }));
    expect((screen.getByTestId("github-token-input") as HTMLInputElement).value).toBe("");
    expect(document.body.innerHTML).not.toContain(FAKE_TOKEN);
    again.view.unmount();
  });

  it("clears the field on a failed submit too, with one sentence", async () => {
    const { client } = await mount(status({ hosts: [] }));
    type("github-token-input", FAKE_TOKEN);
    fireEvent.click(screen.getByTestId("github-token-submit"));
    await flush();
    client.nth("forge.setToken", 0).reject(new RpcError("gh login failed", "token_invalid", { detail: "bad credentials" }));
    await flush();
    expect((screen.getByTestId("github-token-input") as HTMLInputElement).value).toBe("");
    expect(document.body.innerHTML).not.toContain(FAKE_TOKEN);
  });

  it("never stores the token", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const { client, view } = await mount(status({ hosts: [] }));
    type("github-token-input", FAKE_TOKEN);
    fireEvent.click(screen.getByTestId("github-token-submit"));
    await flush();
    client.nth("forge.setToken", 0).resolve({ status: status() });
    await flush();
    const stored = JSON.stringify(setItem.mock.calls);
    expect(stored).not.toContain(FAKE_TOKEN);
    expect(JSON.stringify(window.localStorage)).not.toContain(FAKE_TOKEN);
    expect(JSON.stringify(window.sessionStorage)).not.toContain(FAKE_TOKEN);
    expect(window.location.href).not.toContain(FAKE_TOKEN);
    view.unmount();
  });

  it("shows a typed error in one sentence", async () => {
    for (const [code, sentence] of [
      ["token_invalid", "GitHub rejected that token."],
      ["token_scopes", "That token is missing permissions."],
      ["insecure_channel", "This connection isn't encrypted"],
    ] as const) {
      const { client, view } = await mount(status({ hosts: [] }));
      type("github-token-input", FAKE_TOKEN);
      fireEvent.click(screen.getByTestId("github-token-submit"));
    await flush();
      // A daemon message that mentions the word "token" is replaced by our fixed sentence.
      client.nth("forge.setToken", 0).reject(new RpcError("the token was refused", code, { detail: "x\ny" }));
      await flush();
      const error = screen.getByTestId("github-token-error");
      expect(error).toHaveTextContent(sentence);
      expect(error.textContent).not.toContain("\n");
      expect(error.textContent).not.toContain(FAKE_TOKEN);
      view.unmount();
    }
  });

  it("logs out of a host after a confirm", async () => {
    const { client } = await mount(status());
    fireEvent.click(screen.getByTestId("github-logout-github.com"));
    expect(client.calls.some((c) => c.method === "forge.logout")).toBe(false);
    fireEvent.click(screen.getByTestId("github-logout-dialog-confirm"));
    await flush();
    expect(client.nth("forge.logout", 0).params).toEqual({ host: "github.com" });
    client.nth("forge.logout", 0).resolve({ status: status({ hosts: [] }) });
    await flush();
    expect(screen.queryByTestId("github-host-github.com")).not.toBeInTheDocument();
    expect(screen.getByTestId("github-signed-out")).toBeInTheDocument();
  });
});
