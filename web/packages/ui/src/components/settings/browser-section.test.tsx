import { act } from "react";
import fs from "node:fs";
import path from "node:path";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/browser-section";
import {
  BROWSER_RESIDUAL_LOCAL_KEY,
  BROWSER_RESIDUAL_SHELL_KEY,
  BROWSER_RESIDUAL_WINDOWS_KEY,
} from "@/lib/browser-settings-text";
import { BROWSER_SETTINGS_POLL_MS } from "@/hooks/use-browser-settings";
import { daemonSentence } from "@/lib/browser-settings-text";
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
  it("registers once under Safety & usage at order 192", () => {
    const all = listSettingsSections().filter((s) => s.id === "browser");
    expect(all).toHaveLength(1);
    expect(all[0].label).toBe("Browser");
    expect(all[0].groupLabel).toBe("Safety & usage");
    expect(all[0].order).toBe(192);
  });

  it("starts Off with an empty allowlist, no live indicator and makes only the get call (AC2, AC9)", async () => {
    const s = stateful();
    const d = renderSection(s.handler);
    await ready();
    expect(screen.getByTestId("browser-switch")).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("browser-switch-state")).toHaveTextContent("Off");
    expect(screen.queryByTestId("browser-live")).toBeNull();
    expect(screen.getByTestId("browser-idle")).toHaveTextContent("Not in use right now.");
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
    await waitFor(() => expect(screen.getByTestId("browser-switch-error")).toHaveTextContent("Workspace is read-only."));
    expect(screen.getByTestId("browser-switch")).toHaveAttribute("aria-checked", "false");
    expect(writes(d)).toEqual([{ method: "browser.settings.set", params: { workspaceId: 1, enabled: true } }]);
  });

  it("adds and removes hosts through allowAdd / allowRemove and shows the daemon's refusal verbatim (AC4)", async () => {
    const s = stateful({ enabled: true }, (h) => (h.includes("*") ? `browser: allowlist entry "${h}" must be one exact host` : null));
    // the stateful daemon words it like the real one: "browser.settings.set: browser: allowlist entry ..."
    const d = renderSection(s.handler);
    await ready();
    const input = screen.getByTestId("browser-allow-input");
    fireEvent.change(input, { target: { value: "intranet.example" } });
    fireEvent.click(screen.getByTestId("browser-allow-add"));
    await waitFor(() => expect(within(screen.getByTestId("browser-allow-list")).getByText("intranet.example")).toBeInTheDocument());
    fireEvent.change(input, { target: { value: "*.example.com" } });
    fireEvent.click(screen.getByTestId("browser-allow-add"));
    await waitFor(() => expect(screen.getByTestId("browser-allow-error")).toHaveTextContent('must be one exact host'));
    // the daemon's sentence only: no method or package label in front of it
    expect(screen.getByTestId("browser-allow-error").textContent).toBe('Allowlist entry "*.example.com" must be one exact host.');
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
    expect(screen.getByTestId("browser-switch-state")).toHaveTextContent("On, not in effect");
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

  it("strips the method and package labels, capitalises the first letter and ends with a full stop", () => {
    const table: [string, string][] = [
      ['browser.settings.set: browser: allowlist entry "*.x" must be one exact host', 'Allowlist entry "*.x" must be one exact host.'],
      ["browser.settings.set: at most 50 hosts may be allowed (51 given)", "At most 50 hosts may be allowed (51 given)."],
      ["browser.settings.get: workspace is read-only", "Workspace is read-only."],
      ["browser.data.clear: not available.", "Not available."],
      ["something else: entirely", "Something else: entirely."],
      ["the browser: still here", "The browser: still here."],
      ["Already a sentence!", "Already a sentence!"],
      ["Is it?", "Is it?"],
      ["  padded  ", "Padded."],
      ["", ""],
    ];
    for (const [raw, want] of table) expect(daemonSentence(raw), raw).toBe(want);
  });

  it("the profile card lays its buttons out by the CARD's width: stacked under the text until it is wide (container query, not the viewport)", async () => {
    renderSection(stateful({ sessionLive: true }).handler);
    await ready();
    const row = screen.getByTestId("browser-profile-row");
    expect(row.className).toContain("@container/profile");
    const flex = row.firstElementChild as HTMLElement;
    expect(flex.className).toContain("flex-col");
    expect(flex.className).toContain("@2xl/profile:flex-row");
    expect(flex.className).not.toMatch(/(^|\s)(sm|md|lg):flex-row/); // never keyed to the viewport
    expect(screen.getByTestId("browser-profile-text").className).toContain("min-w-0");
  });

  it("the live line is a polite status region", async () => {
    renderSection(stateful({ sessionLive: true }).handler);
    await ready();
    const line = screen.getByTestId("browser-live-line");
    expect(line).toHaveAttribute("role", "status");
    expect(line).toHaveAttribute("aria-live", "polite");
  });

  it("On says not in effect when no browser is installed or the version does not offer it", async () => {
    renderSection(stateful({ enabled: true, chromeInstalled: false }).handler);
    await ready();
    expect(screen.getByTestId("browser-switch-state")).toHaveTextContent("On, not in effect");
  });

  it("a failed refresh keeps the view but says it may be out of date", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let fail = false;
      const s = stateful({ enabled: true });
      renderSection((m, p) => {
        if (m === "browser.settings.get" && fail) throw new RpcError("browser.settings.get: daemon is restarting", "unavailable");
        return s.handler(m, p);
      });
      await ready();
      expect(screen.queryByTestId("browser-stale")).toBeNull();
      fail = true;
      await act(async () => {
        await vi.advanceTimersByTimeAsync(BROWSER_SETTINGS_POLL_MS + 100);
      });
      await waitFor(() => expect(screen.getByTestId("browser-stale")).toHaveTextContent("may be out of date: Daemon is restarting."));
      expect(screen.getByTestId("browser-switch")).toHaveAttribute("aria-checked", "true"); // the last good view stays
    } finally {
      vi.useRealTimers();
    }
  });

  it("the first read failing shows the error with Retry that reads again", async () => {
    let fail = true;
    const s = stateful();
    const d = renderSection((m, p) => {
      if (m === "browser.settings.get" && fail) throw new RpcError("browser.settings.get: daemon is restarting", "unavailable");
      return s.handler(m, p);
    });
    await waitFor(() => expect(screen.getByTestId("browser-load-error")).toHaveTextContent("Daemon is restarting."));
    fail = false;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await ready();
    expect(d.calls.filter((c) => c.method === "browser.settings.get").length).toBeGreaterThanOrEqual(2);
  });

  it("with the kill switch on an add still sends allowAdd (the list is kept while it is off)", async () => {
    const d = renderSection(stateful({ killSwitch: true }).handler);
    await ready();
    fireEvent.change(screen.getByTestId("browser-allow-input"), { target: { value: "intranet.example" } });
    fireEvent.click(screen.getByTestId("browser-allow-add"));
    await waitFor(() => expect(writes(d)).toEqual([{ method: "browser.settings.set", params: { workspaceId: 1, allowAdd: ["intranet.example"] } }]));
  });

  it("a draft typed for one workspace is gone after switching, and a change goes to the workspace shown", async () => {
    const d = renderSection((m, p) => {
      const id = (p as { workspaceId?: number } | undefined)?.workspaceId ?? 1;
      if (m === "browser.settings.get") return browserView({ workspaceId: id, allowHosts: id === 2 ? ["b.example"] : [] });
      if (m === "browser.settings.set") return browserView({ workspaceId: id, enabled: true });
      return undefined;
    });
    await ready();
    const choose = async (name: string) => {
      await act(async () => {
        fireEvent.keyDown(screen.getByTestId("browser-workspace-select"), { key: "Enter" });
      });
      fireEvent.click(await screen.findByRole("option", { name }));
    };
    // Visit beta once so its view is CACHED: switching back to it later keeps the editor mounted (no loading gap
    // to unmount it), which is exactly when a draft typed for another workspace would survive without the key.
    await choose("beta");
    await waitFor(() => expect(within(screen.getByTestId("browser-allow-list")).getByText("b.example")).toBeInTheDocument());
    await choose("alpha");
    await waitFor(() => expect(screen.getByTestId("browser-allow-empty")).toBeInTheDocument());
    fireEvent.change(screen.getByTestId("browser-allow-input"), { target: { value: "10.0.0.5" } });
    await choose("beta");
    await waitFor(() => expect(within(screen.getByTestId("browser-allow-list")).getByText("b.example")).toBeInTheDocument());
    expect(screen.getByTestId("browser-allow-input")).toHaveValue("");
    fireEvent.click(screen.getByTestId("browser-switch"));
    await waitFor(() => expect(writes(d)).toEqual([{ method: "browser.settings.set", params: { workspaceId: 2, enabled: true } }]));
  });
});
