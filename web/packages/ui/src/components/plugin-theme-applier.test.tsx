import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PluginThemeApplier } from "@/components/plugin-theme-applier";
import { AppearanceSection } from "@/components/settings/appearance-section";
import { ThemeProvider, useTheme } from "@/hooks/use-theme";
import { PLUGIN_THEME_STORAGE_KEY, applyStoredPluginTheme } from "@/lib/plugin-theme";
import { PLUGIN_THEME_TOKENS, type PluginTheme } from "@/lib/plugin-contrib";
import { usePluginThemeStore } from "@/stores/plugin-theme-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";

const set = (value: string) => Object.fromEntries(PLUGIN_THEME_TOKENS.map((t) => [t, value]));
const theme = (id: string, schemes: PluginTheme["schemes"], plugin = "acme"): PluginTheme => ({
  plugin,
  id,
  qualified: `${plugin}:${id}`,
  label: id[0]!.toUpperCase() + id.slice(1),
  schemes,
});
const DUSK = theme("dusk", { light: set("#aabbcc"), dark: set("#112233") });
const DARK_ONLY = theme("night", { dark: set("#010203") });
/** The plugin-set properties on the root (the page also keeps its font-scale variables there). */
const applied = () => PLUGIN_THEME_TOKENS.filter((t) => document.documentElement.style.getPropertyValue(t) !== "");
const bg = () => document.documentElement.style.getPropertyValue("--background");

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

let pref: ReturnType<typeof useTheme> | null = null;
function Probe({ client }: { client: FakeWsClient }) {
  pref = useTheme();
  void client;
  return (
    <>
      <PluginThemeApplier />
      <AppearanceSection />
    </>
  );
}

async function mount(themes: PluginTheme[] | "error" = [DUSK, DARK_ONLY]) {
  const client = new FakeWsClient();
  renderWithClientProp(
    <ThemeProvider>
      <Probe client={client} />
    </ThemeProvider>,
  );
  if (themes === "error") client.nth("plugin.themes", 0).reject(new Error("unknown method"));
  else client.nth("plugin.themes", 0).resolve(themes);
  await flush();
  return client;
}

beforeEach(() => {
  window.localStorage.clear();
  usePluginThemeStore.getState().reload();
  window.matchMedia = vi.fn().mockReturnValue({ matches: false, addEventListener: () => {}, removeEventListener: () => {} }) as unknown as typeof window.matchMedia;
});
afterEach(() => {
  cleanup();
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.cssText = "";
});

describe("plugin themes", () => {
  it("TestTheme_AppliesPerScheme: applies in a declared scheme and leaves the other scheme untouched", async () => {
    await mount();
    fireEvent.click(screen.getByTestId("settings-plugin-theme-acme:night"));
    expect(usePluginThemeStore.getState().value?.id).toBe("acme:night");
    // Light is the resolved scheme and the theme declares only dark: nothing is applied.
    expect(bg()).toBe("");
    expect(screen.getByTestId("settings-plugin-theme-note")).toHaveTextContent("Night has no light variant");
    act(() => pref!.setPreference("dark"));
    await flush();
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(bg()).toBe("#010203");
    expect(PLUGIN_THEME_TOKENS.every((t) => document.documentElement.style.getPropertyValue(t) === "#010203")).toBe(true);
    expect(screen.queryByTestId("settings-plugin-theme-note")).toBeNull();
    // Back to light: every token handed back to index.css, none left behind.
    act(() => pref!.setPreference("light"));
    await flush();
    expect(applied()).toEqual([]);
  });

  it("a theme with both schemes applies the matching set on each switch", async () => {
    await mount();
    fireEvent.click(screen.getByTestId("settings-plugin-theme-acme:dusk"));
    expect(bg()).toBe("#aabbcc");
    act(() => pref!.setPreference("dark"));
    await flush();
    expect(bg()).toBe("#112233");
    act(() => pref!.setPreference("light"));
    await flush();
    expect(bg()).toBe("#aabbcc");
  });

  it("only the canonical tokens are ever set on the root, as properties and never as style text", async () => {
    await mount();
    fireEvent.click(screen.getByTestId("settings-plugin-theme-acme:dusk"));
    const style = document.documentElement.style;
    expect(applied().length).toBe(PLUGIN_THEME_TOKENS.length);
    for (let i = 0; i < style.length; i++) {
      const name = style.item(i);
      if (name.startsWith("--") && !name.startsWith("--font-scale")) expect(PLUGIN_THEME_TOKENS).toContain(name);
    }
    expect(document.querySelectorAll("style[data-plugin-theme]").length).toBe(0);
  });

  it("TestTheme_FallbackWhenPluginDisabled: plugin.updated + an empty list drops the theme, the tokens and the stored choice", async () => {
    const client = await mount();
    fireEvent.click(screen.getByTestId("settings-plugin-theme-acme:dusk"));
    expect(bg()).toBe("#aabbcc");
    expect(window.localStorage.getItem(PLUGIN_THEME_STORAGE_KEY)).toContain("acme:dusk");
    act(() => client.pushNotification("plugin.updated", { id: "acme", globalEnabled: true }));
    await flush();
    client.nth("plugin.themes", 1).resolve([]);
    await flush();
    expect(bg()).toBe("");
    expect(applied()).toEqual([]);
    expect(usePluginThemeStore.getState().value).toBeNull();
    expect(window.localStorage.getItem(PLUGIN_THEME_STORAGE_KEY)).toBeNull();
    expect(screen.queryByTestId("settings-plugin-themes")).toBeNull();
    expect(screen.getByTestId("settings-theme-system")).toHaveAttribute("aria-pressed", "true");
  });

  it("an unreachable or old daemon keeps the remembered theme instead of dropping it", async () => {
    usePluginThemeStore.getState().set({ id: DUSK.qualified, theme: DUSK });
    await mount("error");
    expect(bg()).toBe("#aabbcc");
    expect(usePluginThemeStore.getState().value?.id).toBe("acme:dusk");
  });

  it("applyStoredPluginTheme paints the remembered theme before React renders", () => {
    window.localStorage.setItem(PLUGIN_THEME_STORAGE_KEY, JSON.stringify({ id: DUSK.qualified, theme: DUSK }));
    document.documentElement.classList.add("dark");
    applyStoredPluginTheme();
    expect(bg()).toBe("#112233");
  });

  it("a plugin update's new tokens replace the applied ones and the remembered copy", async () => {
    const client = await mount();
    fireEvent.click(screen.getByTestId("settings-plugin-theme-acme:dusk"));
    act(() => client.pushNotification("plugin.updated", { id: "acme", globalEnabled: true }));
    await flush();
    client.nth("plugin.themes", 1).resolve([theme("dusk", { light: set("#ddeeff") }), DARK_ONLY]);
    await flush();
    expect(bg()).toBe("#ddeeff");
    expect(usePluginThemeStore.getState().value?.theme.schemes.light!["--background"]).toBe("#ddeeff");
  });

  it("never applies a theme the client cannot vouch for: unsafe value, extra token, incomplete set", async () => {
    const hostile = theme("evil", { light: { ...set("#fff"), "--background": "url(https://evil.example/x)" } });
    const extra = theme("extra", { light: { ...set("#fff"), "--evil": "#000" } });
    const partial = theme("partial", { light: Object.fromEntries(PLUGIN_THEME_TOKENS.slice(1).map((t) => [t, "#fff"])) });
    await mount([hostile, extra, partial]);
    for (const id of ["evil", "extra", "partial"]) {
      fireEvent.click(screen.getByTestId(`settings-plugin-theme-acme:${id}`));
      expect(applied(), id).toEqual([]);
    }
  });

  it("without plugin themes the Appearance section has no plugin theme row", async () => {
    await mount([]);
    expect(screen.queryByTestId("settings-plugin-themes")).toBeNull();
    expect(screen.getByTestId("settings-theme-light")).toBeInTheDocument();
  });
});
