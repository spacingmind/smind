import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProviderSecrets } from "@/components/providers/provider-secrets";
import type { ProviderInfo, ProviderSecretInfo } from "@/lib/types";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const SENTINEL = "sk-test-SENTINEL-0002";

function inst(secrets?: ProviderSecretInfo[]): ProviderInfo {
  return { id: "zai-claude", label: "Z.ai", custom: true, runner: "claude-native", extends: "claude-native", secrets };
}

const SET: ProviderSecretInfo = { name: "api-key", env: "ANTHROPIC_AUTH_TOKEN", set: true, fingerprint: "ab12" };

function setup(info: ProviderInfo): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  d.providers = { providers: [info] };
  renderWithDaemon(<ProviderSecrets info={info} hasClient />, d);
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function calls(d: FakeUsageDaemon, method: string): unknown[] {
  return d.calls.filter((c) => c.method === method).map((c) => c.params);
}

describe("ProviderSecrets", () => {
  it("renders nothing without secrets", () => {
    setup(inst());
    expect(screen.queryByTestId("provider-secrets-zai-claude")).toBeNull();
    expect(document.body.textContent).toBe("");
  });

  it("shows set with the fingerprint and never a value", () => {
    setup(inst([SET]));
    const row = screen.getByTestId("provider-secret-zai-claude-api-key");
    expect(row).toHaveTextContent("ANTHROPIC_AUTH_TOKEN");
    expect(row).toHaveTextContent("Set · ••••ab12");
    expect(screen.getByTestId("provider-secret-replace-api-key")).toHaveTextContent("Replace key");
    expect(screen.getByTestId("provider-secret-clear-api-key")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("sk-");
  });

  it("shows not set with a Set key action", () => {
    setup(inst([{ name: "api-key", env: "ANTHROPIC_AUTH_TOKEN", set: false }]));
    expect(screen.getByTestId("provider-secret-zai-claude-api-key")).toHaveTextContent("Not set");
    expect(screen.getByTestId("provider-secret-replace-api-key")).toHaveTextContent("Set key");
    expect(screen.queryByTestId("provider-secret-clear-api-key")).toBeNull();
  });

  it("the replace dialog input is a password field, empty on open", () => {
    setup(inst([SET]));
    fireEvent.click(screen.getByTestId("provider-secret-replace-api-key"));
    const input = screen.getByTestId("provider-secret-input");
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveAttribute("autocomplete", "off");
    expect(input).toHaveValue("");
    expect(screen.getByTestId("provider-secret-save")).toBeDisabled();
  });

  it("saving sends instanceId name value once, closes, and the value is not in the document afterwards", async () => {
    const d = setup(inst([SET]));
    fireEvent.click(screen.getByTestId("provider-secret-replace-api-key"));
    fireEvent.change(screen.getByTestId("provider-secret-input"), { target: { value: SENTINEL } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-secret-save"));
    });
    await flush();
    expect(calls(d, "provider.setSecret")).toEqual([{ instanceId: "zai-claude", name: "api-key", value: SENTINEL }]);
    await waitFor(() => expect(screen.queryByTestId("provider-secret-input")).toBeNull());
    expect(document.body.textContent).not.toContain(SENTINEL);
    fireEvent.click(screen.getByTestId("provider-secret-replace-api-key"));
    expect(screen.getByTestId("provider-secret-input")).toHaveValue("");
  });

  it("a daemon refusal shows its text and leaves the field empty", async () => {
    const d = new FakeUsageDaemon([]);
    d.secretError = new Error("secret store is locked");
    const info = inst([SET]);
    d.providers = { providers: [info] };
    renderWithDaemon(<ProviderSecrets info={info} hasClient />, d);
    fireEvent.click(screen.getByTestId("provider-secret-replace-api-key"));
    fireEvent.change(screen.getByTestId("provider-secret-input"), { target: { value: SENTINEL } });
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-secret-save"));
    });
    await flush();
    expect(await screen.findByTestId("provider-secret-error")).toHaveTextContent("secret store is locked");
    expect(screen.getByTestId("provider-secret-input")).toHaveValue("");
    expect(document.body.textContent).not.toContain(SENTINEL);
  });

  it("clear key asks to confirm then calls provider.clearSecret", async () => {
    const d = setup(inst([SET]));
    fireEvent.click(screen.getByTestId("provider-secret-clear-api-key"));
    expect(screen.getByText("Runs on this provider will fail until a new key is set.")).toBeInTheDocument();
    expect(calls(d, "provider.clearSecret")).toEqual([]);
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-secret-clear-dialog-api-key-confirm"));
    });
    await flush();
    expect(calls(d, "provider.clearSecret")).toEqual([{ instanceId: "zai-claude", name: "api-key" }]);
  });
});
