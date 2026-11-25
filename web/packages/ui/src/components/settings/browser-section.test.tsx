import { act } from "react";
import fs from "node:fs";
import path from "node:path";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/browser-section";
import {
  BROWSER_RESIDUAL_LOCAL_KEY,
  BROWSER_RESIDUAL_SHELL_KEY,
  BROWSER_RESIDUAL_WINDOWS_KEY,
} from "@/lib/browser-settings-text";
import { RpcError } from "@/lib/ws-client";
import { browserDaemon, browserView } from "@/test/browser-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import type { BrowserSettingsView } from "@/lib/browser-settings-types";

function renderSection(handler: (method: string, params: unknown) => unknown) {
  const d = browserDaemon(handler);
  const section = listSettingsSections().find((s) => s.id === "browser");
  if (!section) throw new Error("browser section did not register");
  renderWithDaemon(<>{section.render({ client: d.client as never, events: null })}</>, d);
  return d;
}

/** A daemon whose view is mutable and whose set applies the three fields the UI may send. */
function stateful(initial: Partial<BrowserSettingsView> = {}, refuse?: (host: string) => string | null) {
  let view = browserView(initial);
  const handler = (method: string, params: unknown) => {
    const p = (params ?? {}) as { enabled?: boolean; allowAdd?: string[]; allowRemove?: string[] };
    if (method === "browser.settings.get") return view;
    if (method === "browser.settings.set") {
      if (typeof p.enabled === "boolean") view = { ...view, enabled: p.enabled };
      for (const h of p.allowAdd ?? []) {
        const why = refuse?.(h);
        if (why) throw new RpcError(`browser.settings.set: ${why}`, "invalid");
        view = { ...view, allowHosts: [...view.allowHosts, h] };
      }
      for (const h of p.allowRemove ?? []) view = { ...view, allowHosts: view.allowHosts.filter((x) => x !== h) };
      return view;
    }
    if (method === "browser.data.clear") return {};
    return undefined;
  };
  return { handler, set: (over: Partial<BrowserSettingsView>) => (view = { ...view, ...over }) };
}

async function ready() {
  await waitFor(() => expect(screen.getByTestId("browser-switch")).toBeInTheDocument());
}

const writes = (d: { calls: { method: string; params?: unknown }[] }) =>
  d.calls.filter((c) => c.method.startsWith("browser.") && c.method !== "browser.settings.get");

describe("browser section", () => {
  it("registers once under Profiles & providers at order 197", () => {
    const all = listSettingsSections().filter((s) => s.id === "browser");
    expect(all).toHaveLength(1);
    expect(all[0].label).toBe("Browser");
    expect(all[0].groupLabel).toBe("Profiles & providers");
    expect(all[0].order).toBe(197);
  });

  it("starts Off with an empty allowlist, no live indicator and makes only the get call (AC2, AC9)", async () => {
    const s = stateful();
    const d = renderSection(s.handler);
    await ready();
    expect(screen.getByTestId("browser-switch")).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("browser-switch-state")).toHaveTextContent("Off");
    expect(screen.queryByTestId("browser-live")).toBeNull();
    expect(screen.getByTestId("browser-allow-empty")).toBeInTheDocument();
    expect(d.calls.filter((c) => c.method.startsWith("browser.")).map((c) => c.method)).toEqual(["browser.settings.get"]);
  });

  it("the switch sends exactly {workspaceId, enabled} and shows the answered state (AC3)", async () => {
    const s = stateful();
    const d = renderSection(s.handler);
    await ready();
    fireEvent.click(screen.getByTestId("browser-switch"));
    await waitFor(() => expect(screen.getByTestId("browser-switch-state")).toHaveTextContent("On"));
    expect(writes(d)).toEqual([{ method: "browser.settings.set", params: { workspaceId: 1, enabled: true } }]);
    fireEvent.click(screen.getByTestId("browser-switch"));
    await waitFor(() => expect(screen.getByTestId("browser-switch-state")).toHaveTextContent("Off"));
    expect(writes(d)[1]).toEqual({ method: "browser.settings.set", params: { workspaceId: 1, enabled: false } });
  });

  it("a refused switch shows the daemon's text and the stored state (AC3)", async () => {
    let refuse = false;
    const d = renderSection((m) => {
      if (m === "browser.settings.get") return browserView();
      if (m === "browser.settings.set" && refuse) throw new RpcError("browser.settings.set: workspace is read-only", "forbidden");
      return undefined;
    });
    await ready();
    refuse = true;
    fireEvent.click(screen.getByTestId("browser-switch"));
    await waitFor(() => expect(screen.getByTestId("browser-switch-error")).toHaveTextContent("workspace is read-only"));
    expect(screen.getByTestId("browser-switch")).toHaveAttribute("aria-checked", "false");
    expect(d.calls.length).toBeGreaterThan(0);
  });

  it("adds and removes hosts through allowAdd / allowRemove and shows the daemon's refusal verbatim (AC4)", async () => {
    const s = stateful({ enabled: true }, (h) => (h.includes("*") ? `browser: allowlist entry "${h}" must be one exact host` : null));
    const d = renderSection(s.handler);
    await ready();
    const input = screen.getByTestId("browser-allow-input");
    fireEvent.change(input, { target: { value: "intranet.example" } });
    fireEvent.click(screen.getByTestId("browser-allow-add"));
    await waitFor(() => expect(within(screen.getByTestId("browser-allow-list")).getByText("intranet.example")).toBeInTheDocument());
    fireEvent.change(input, { target: { value: "*.example.com" } });
    fireEvent.click(screen.getByTestId("browser-allow-add"));
    await waitFor(() => expect(screen.getByTestId("browser-allow-error")).toHaveTextContent('must be one exact host'));
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveValue("*.example.com");
    fireEvent.click(screen.getByRole("button", { name: "Remove intranet.example" }));
    await waitFor(() => expect(screen.getByTestId("browser-allow-empty")).toBeInTheDocument());
    expect(writes(d).map((c) => c.params)).toEqual([
      { workspaceId: 1, allowAdd: ["intranet.example"] },
      { workspaceId: 1, allowAdd: ["*.example.com"] },
      { workspaceId: 1, allowRemove: ["intranet.example"] },
    ]);
  });

  it("the kill switch shows its banner and disables the switch; the allowlist stays editable (AC5)", async () => {
    renderSection(stateful({ killSwitch: true, enabled: true }).handler);
    await ready();
    expect(screen.getByTestId("browser-kill-switch")).toHaveTextContent("switched off by the daemon's configuration");
    expect(screen.getByTestId("browser-switch")).toBeDisabled();
    expect(screen.getByTestId("browser-allow-input")).not.toBeDisabled();
  });

  it("no browser installed shows the daemon's install step; available false says so (AC5)", async () => {
    renderSection(stateful({ chromeInstalled: false, installStep: "Install Chromium with brew.", available: false }).handler);
    await ready();
    expect(screen.getByTestId("browser-no-chrome")).toHaveTextContent("No browser installed");
    expect(screen.getByTestId("browser-install-step")).toHaveTextContent("Install Chromium with brew.");
    expect(screen.getByTestId("browser-unavailable")).toBeInTheDocument();
  });

  it("neither banner shows when the daemon is healthy", async () => {
    renderSection(stateful().handler);
    await ready();
    for (const id of ["browser-kill-switch", "browser-no-chrome", "browser-unavailable"]) expect(screen.queryByTestId(id)).toBeNull();
  });

  it("the live indicator shows only while sessionLive and End session clears the data (AC6)", async () => {
    const s = stateful({ enabled: true, sessionLive: true });
    const d = renderSection(s.handler);
    await ready();
    expect(screen.getByTestId("browser-live")).toHaveTextContent("In use now");
    s.set({ sessionLive: false });
    fireEvent.click(screen.getByTestId("browser-end-session"));
    await waitFor(() => expect(screen.queryByTestId("browser-live")).toBeNull());
    expect(writes(d)).toEqual([{ method: "browser.data.clear", params: { workspaceId: 1 } }]);
  });

  it("Clear browser data asks first and then calls browser.data.clear (AC6)", async () => {
    const d = renderSection(stateful().handler);
    await ready();
    fireEvent.click(screen.getByTestId("browser-clear"));
    expect(screen.getByTestId("browser-clear-dialog")).toBeInTheDocument();
    expect(writes(d)).toEqual([]);
    fireEvent.click(within(screen.getByTestId("browser-clear-dialog")).getByRole("button", { name: "Clear browser data" }));
    await waitFor(() => expect(writes(d)).toEqual([{ method: "browser.data.clear", params: { workspaceId: 1 } }]));
  });

  it("states the three residual limits next to the switch (AC7)", async () => {
    renderSection(stateful().handler);
    await ready();
    expect(screen.getByTestId("browser-residual-local")).toHaveTextContent(BROWSER_RESIDUAL_LOCAL_KEY);
    expect(screen.getByTestId("browser-residual-shell")).toHaveTextContent(BROWSER_RESIDUAL_SHELL_KEY);
    expect(screen.getByTestId("browser-residual-windows")).toHaveTextContent(BROWSER_RESIDUAL_WINDOWS_KEY);
  });

  it("the residual key phrases are also on the public page, so the two cannot drift (AC7)", () => {
    const doc = fs
      .readFileSync(path.resolve(__dirname, "../../../../../../public-docs/browser.md"), "utf8")
      .replace(/\*+/g, "")
      .replace(/\s+/g, " ");
    for (const key of [BROWSER_RESIDUAL_LOCAL_KEY, BROWSER_RESIDUAL_SHELL_KEY, BROWSER_RESIDUAL_WINDOWS_KEY]) {
      expect(doc, key).toContain(key);
    }
  });

  it("lists the providers from the daemon's view, never a fixed list (AC8)", async () => {
    renderSection(stateful({ providers: [{ id: "zz", label: "Zed Agent", reach: "no", why: "no MCP here", images: "unknown" }] }).handler);
    await ready();
    const reach = screen.getByTestId("browser-reach");
    expect(within(reach).getByText("Zed Agent")).toBeInTheDocument();
    expect(within(reach).queryByText("Codex")).toBeNull();
    expect(within(reach).getByText("no MCP here")).toBeInTheDocument();
  });

  it("switching workspace reads that workspace's view and sends its id", async () => {
    const d = renderSection((m, p) => {
      if (m === "browser.settings.get") return browserView({ workspaceId: (p as { workspaceId: number }).workspaceId });
      if (m === "browser.settings.set") return browserView({ workspaceId: (p as { workspaceId: number }).workspaceId, enabled: true });
      return undefined;
    });
    await ready();
    expect(d.calls.find((c) => c.method === "browser.settings.get")?.params).toEqual({ workspaceId: 1 });
    await act(async () => {
      fireEvent.keyDown(screen.getByTestId("browser-workspace-select"), { key: "Enter" });
    });
    // Radix select in jsdom: pick the second workspace through its option.
    const option = await screen.findByRole("option", { name: "beta" });
    fireEvent.click(option);
    await waitFor(() => expect(d.calls.some((c) => c.method === "browser.settings.get" && (c.params as { workspaceId: number }).workspaceId === 2)).toBe(true));
  });
});
