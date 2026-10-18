import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

async function load(isDesktop: boolean) {
  vi.resetModules();
  const openExternal = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({ isDesktop, desktop: { openExternal } }));
  const { TimelineMarkdown } = await import("@/components/timeline/timeline-markdown");
  return { TimelineMarkdown, openExternal };
}

afterEach(() => {
  cleanup();
  vi.doUnmock("@/lib/platform");
});

describe("TimelineMarkdown links", () => {
  it("desktop: an http(s) link opens through the bridge", async () => {
    const { TimelineMarkdown, openExternal } = await load(true);
    render(<TimelineMarkdown content="see [docs](https://example.com/a)" />);
    expect(fireEvent.click(screen.getByRole("link", { name: "docs" }))).toBe(false);
    expect(openExternal).toHaveBeenCalledWith("https://example.com/a");
  });

  it("web: an http(s) link opens in a new tab instead of navigating the app away", async () => {
    const { TimelineMarkdown, openExternal } = await load(false);
    render(<TimelineMarkdown content="see [docs](https://example.com/a)" />);
    const link = screen.getByRole("link", { name: "docs" });
    expect(link).toHaveAttribute("target", "_blank");
    expect(fireEvent.click(link)).toBe(true);
    expect(openExternal).not.toHaveBeenCalled();
  });

  it("an in-page fragment link keeps its default behavior", async () => {
    const { TimelineMarkdown } = await load(true);
    render(<TimelineMarkdown content="[top](#top)" />);
    expect(screen.getByRole("link", { name: "top" })).not.toHaveAttribute("target");
  });
});

describe("MarkdownPreview links", () => {
  it("desktop: a link in the file preview opens through the bridge", async () => {
    vi.resetModules();
    const openExternal = vi.fn(() => Promise.resolve());
    vi.doMock("@/lib/platform", () => ({ isDesktop: true, desktop: { openExternal } }));
    const { MarkdownPreview } = await import("@/components/file-preview");
    render(<MarkdownPreview content="[docs](https://example.com/a)" />);
    expect(fireEvent.click(screen.getByRole("link", { name: "docs" }))).toBe(false);
    expect(openExternal).toHaveBeenCalledWith("https://example.com/a");
  });
});
