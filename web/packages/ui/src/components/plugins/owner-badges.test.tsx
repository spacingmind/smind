import { act } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OwnerBadge } from "@/components/plugins/owner-badge";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/mcp-servers-section";
import "@/components/settings/profiles-section";
import { ProfileContractFields } from "@/components/settings/profile-contract-fields";
import { EMPTY_CONTRACT } from "@/components/settings/profile-contract";
import { takePluginFocus } from "@/lib/plugin-focus";
import type { AgentProfile } from "@/lib/types";
import { ownedMcp, PluginDaemon } from "@/test/plugin-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

function section(id: string) {
  const s = listSettingsSections().find((x) => x.id === id);
  if (!s) throw new Error(`${id} did not register`);
  return s;
}

const USER_MCP = ownedMcp({ id: 1, name: "playwright", owner: null, readOnly: false, url: "", transport: "stdio", command: "npx", args: [] });
const PROFILE = (over: Partial<AgentProfile>): AgentProfile => ({
  ID: 1,
  Name: "Mine",
  Provider: "claude-native",
  PermissionMode: "",
  AutoAccept: false,
  ThinkingLevel: "",
  Notes: "",
  CreatedAt: "2026-10-25T00:00:00Z",
  UpdatedAt: "2026-10-25T00:00:00Z",
  ...over,
});

afterEach(() => {
  takePluginFocus();
});

describe("OwnerBadge", () => {
  it("renders nothing for a row nobody owns", () => {
    const { container } = render(<OwnerBadge owner={null} />);
    expect(container).toBeEmptyDOMElement();
    const { container: c2 } = render(<OwnerBadge owner={undefined} />);
    expect(c2).toBeEmptyDOMElement();
  });

  it("names the plugin, explains where to manage it, and opens its detail in Settings -> Plugins", () => {
    const opened: unknown[] = [];
    const on = (e: Event) => opened.push((e as CustomEvent).detail);
    window.addEventListener("smind:open-settings", on);
    render(<OwnerBadge owner="acme" testId="b" />);
    const badge = screen.getByTestId("b");
    expect(badge).toHaveTextContent("from acme");
    expect(badge).toHaveAttribute("title", "Owned by plugin acme: manage it in Settings → Plugins");
    fireEvent.click(badge);
    window.removeEventListener("smind:open-settings", on);
    expect(opened).toEqual([{ sectionId: "plugins" }]);
    expect(takePluginFocus()).toBe("acme");
  });

  it("link={false} renders no nested button (for a picker option)", () => {
    render(<OwnerBadge owner="acme" link={false} testId="b" />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByTestId("b")).toHaveTextContent("from acme");
  });
});

describe("owned rows in Settings -> MCP servers", () => {
  async function renderMcp(d: PluginDaemon) {
    renderWithDaemon(<>{section("mcp-servers").render({ client: d.client, events: d.events })}</>, d);
    await flush();
  }

  it("labels an owned row from its plugin and makes it read-only (no toggle, no edit/delete menu); user rows keep theirs", async () => {
    const d = new PluginDaemon();
    d.mcp = [USER_MCP, ownedMcp()];
    await renderMcp(d);
    const owned = screen.getByTestId(`mcp-row-${ownedMcp().id}`);
    expect(within(owned).getByText("from acme")).toBeInTheDocument();
    expect(within(owned).getByTestId(`mcp-owner-${ownedMcp().id}`)).toHaveAttribute("title", expect.stringContaining("Settings → Plugins"));
    expect(screen.getByTestId(`mcp-enabled-${ownedMcp().id}`)).toBeDisabled();
    expect(screen.queryByTestId(`mcp-menu-${ownedMcp().id}`)).toBeNull();
    // the user's own row is untouched
    expect(screen.getByTestId("mcp-enabled-1")).not.toBeDisabled();
    expect(screen.getByTestId("mcp-menu-1")).toBeInTheDocument();
    expect(within(screen.getByTestId("mcp-row-1")).queryByText(/^from /)).toBeNull();
  });

  it("an owned row's controls never reach mcp.setEnabled/update/delete", async () => {
    const d = new PluginDaemon();
    d.mcp = [ownedMcp()];
    await renderMcp(d);
    fireEvent.click(screen.getByTestId(`mcp-enabled-${ownedMcp().id}`));
    await flush();
    expect(d.countCalls("mcp.setEnabled")).toBe(0);
  });

  it("a plugin disabled: plugin.updated refetches and its rows vanish", async () => {
    const d = new PluginDaemon();
    d.mcp = [USER_MCP, ownedMcp()];
    await renderMcp(d);
    expect(screen.getByTestId(`mcp-row-${ownedMcp().id}`)).toBeInTheDocument();
    d.mcp = [USER_MCP];
    act(() => d.emit("plugin.updated", { id: "acme", globalEnabled: true }));
    await flush();
    await waitFor(() => expect(screen.queryByTestId(`mcp-row-${ownedMcp().id}`)).toBeNull());
    expect(screen.getByTestId("mcp-row-1")).toBeInTheDocument();
    expect(d.countCalls("mcp.list")).toBe(2);
  });
});

describe("owned rows in Settings -> Agents", () => {
  it("labels an owned agent, hides its edit/delete menu, and keeps the user's agent editable", async () => {
    const d = new PluginDaemon();
    d.profiles = [PROFILE({ ID: 1 }), PROFILE({ ID: 1099511627778, Name: "Acme reviewer", owner: "acme", readOnly: true })];
    renderWithDaemon(<>{section("agents").render({ client: d.client, events: d.events })}</>, d);
    await flush();
    const owned = screen.getByTestId("profile-row-1099511627778");
    expect(within(owned).getByText("from acme")).toBeInTheDocument();
    expect(screen.queryByTestId("profile-menu-1099511627778")).toBeNull();
    expect(screen.getByTestId("profile-menu-1")).toBeInTheDocument();
    expect(within(screen.getByTestId("profile-row-1")).queryByText(/^from /)).toBeNull();
  });

  it("a plugin disabled: plugin.updated refetches profile.list and the owned agent vanishes", async () => {
    const d = new PluginDaemon();
    const owned = PROFILE({ ID: 1099511627778, Name: "Acme reviewer", owner: "acme", readOnly: true });
    d.profiles = [PROFILE({ ID: 1 }), owned];
    renderWithDaemon(<>{section("agents").render({ client: d.client, events: d.events })}</>, d);
    await flush();
    d.profiles = [PROFILE({ ID: 1 })];
    act(() => d.emit("plugin.updated", { id: "acme", globalEnabled: true }));
    await flush();
    await waitFor(() => expect(screen.queryByTestId("profile-row-1099511627778")).toBeNull());
  });
});

describe("skill suggestions in the agent editor", () => {
  it("lists bundled and plugin skills; a plugin skill carries the badge and a click names it", async () => {
    const d = new PluginDaemon();
    d.skills = [
      { name: "acme:review", source: "plugin", owner: "acme", readOnly: true, description: "Reviews" },
      { name: "smind-peer", source: "bundled", owner: null, readOnly: true },
    ];
    const setForm = vi.fn();
    renderWithDaemon(<ProfileContractFields idPrefix="p" form={{ ...EMPTY_CONTRACT }} setForm={setForm} otherProfiles={[]} />, d);
    await flush();
    const chips = screen.getByTestId("p-skills-available");
    expect(within(chips).getByTestId("p-skill-owner-acme:review")).toHaveTextContent("from acme");
    expect(within(chips).queryByTestId("p-skill-owner-smind-peer")).toBeNull();
    fireEvent.click(within(chips).getByTestId("p-skill-acme:review"));
    expect(setForm).toHaveBeenCalledOnce();
    const next = (setForm.mock.calls[0]![0] as (f: typeof EMPTY_CONTRACT) => typeof EMPTY_CONTRACT)({ ...EMPTY_CONTRACT, skills: "smind-peer" });
    expect(next.skills).toBe("smind-peer, acme:review");
  });
});
