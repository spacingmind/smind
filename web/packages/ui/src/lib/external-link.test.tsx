import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const URL_ = "https://claude.ai/oauth/authorize?state=abc";

async function load(isDesktop: boolean) {
  vi.resetModules();
  const openExternal = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({ isDesktop, desktop: { openExternal } }));
  const mod = await import("@/lib/external-link");
  return { ...mod, openExternal };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("external links", () => {
  it("desktop: openExternalUrl goes through the desktop bridge, never window.open", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const { openExternalUrl, openExternal } = await load(true);
    openExternalUrl(URL_);
    expect(openExternal).toHaveBeenCalledWith(URL_);
    expect(open).not.toHaveBeenCalled();
  });

  it("web: openExternalUrl uses window.open with noopener", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const { openExternalUrl, openExternal } = await load(false);
    openExternalUrl(URL_);
    expect(open).toHaveBeenCalledWith(URL_, "_blank", "noopener,noreferrer");
    expect(openExternal).not.toHaveBeenCalled();
  });

  it("desktop: a link click is intercepted and opened through the bridge", async () => {
    const { externalLinkProps, openExternal } = await load(true);
    render(<a {...externalLinkProps(URL_)}>login</a>);
    const notPrevented = fireEvent.click(screen.getByRole("link", { name: "login" }));
    expect(notPrevented).toBe(false);
    expect(openExternal).toHaveBeenCalledWith(URL_);
  });

  it("web: a link click is left to the browser (real href, new tab)", async () => {
    const { externalLinkProps, openExternal } = await load(false);
    render(<a {...externalLinkProps(URL_)}>login</a>);
    const link = screen.getByRole("link", { name: "login" });
    expect(link).toHaveAttribute("href", URL_);
    expect(link).toHaveAttribute("target", "_blank");
    expect(fireEvent.click(link)).toBe(true);
    expect(openExternal).not.toHaveBeenCalled();
  });
});
