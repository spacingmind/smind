import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AttachmentPill } from "@/components/composer/attachment-pill";
import { AttachmentTray } from "@/components/composer/attachment-tray";
import type { DraftAttachment } from "@/stores/composer-attachments-store";

const image: DraftAttachment = { localId: "a", file: new File(["x"], "shot.png"), kind: "image", name: "shot.png", mimeType: "image/png", size: 2048, previewUrl: "blob:shot" };
const doc: DraftAttachment = { localId: "b", file: new File(["x"], "report.pdf"), kind: "file", name: "report.pdf", mimeType: "application/pdf", size: 90311 };

function stubHover(hover: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: q === "(hover: none)" ? !hover : false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} }));
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AttachmentPill", () => {
  it("renders an image as a thumbnail and a file as a two-line pill", () => {
    render(<AttachmentPill item={image} onRemove={() => {}} />);
    const img = screen.getByRole("img", { name: "shot.png" });
    expect(img).toHaveAttribute("src", "blob:shot");
    expect(img.className).toContain("size-12");
    cleanup();
    render(<AttachmentPill item={doc} onRemove={() => {}} />);
    expect(screen.queryByRole("img", { name: "report.pdf" })).toBeNull();
    expect(screen.getByText("report.pdf")).toBeInTheDocument();
    expect(screen.getByText("PDF · 88 KiB")).toBeInTheDocument();
  });

  it("labels a file by its extension, not by the MIME subtype", () => {
    render(<AttachmentPill item={{ ...doc, name: "notes.txt", mimeType: "text/plain", size: 550 }} onRemove={() => {}} />);
    expect(screen.getByText("TXT · 550 B")).toBeInTheDocument();
    cleanup();
    render(<AttachmentPill item={{ ...doc, name: "Makefile", mimeType: "text/plain", size: 10 }} onRemove={() => {}} />);
    expect(screen.getByText("PLAIN · 10 B")).toBeInTheDocument();
  });

  it("close badge: hidden until hover/focus on pointer devices, always shown on touch", () => {
    stubHover(true);
    render(<AttachmentPill item={doc} onRemove={() => {}} />);
    expect(screen.getByTestId("attachment-pill-close").className).toContain("md:opacity-0");
    expect(screen.getByTestId("attachment-pill-close").className).toContain("md:group-hover:opacity-100");
    cleanup();
    stubHover(false);
    render(<AttachmentPill item={doc} onRemove={() => {}} />);
    expect(screen.getByTestId("attachment-pill-close").className).not.toContain("opacity-0");
  });

  it("removes on the close badge, and on Backspace/Delete of the focused pill", () => {
    const onRemove = vi.fn();
    render(<AttachmentPill item={doc} onRemove={onRemove} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove report.pdf" }));
    expect(onRemove).toHaveBeenCalledTimes(1);
    const pill = screen.getByTestId("attachment-pill");
    fireEvent.keyDown(pill, { key: "Backspace" });
    fireEvent.keyDown(pill, { key: "Delete" });
    expect(onRemove).toHaveBeenCalledTimes(3);
  });

  it("an error pill shows the warning and the reason in destructive color", () => {
    render(<AttachmentPill item={{ ...image, error: "Larger than 5 MiB" }} onRemove={() => {}} />);
    const pill = screen.getByTestId("attachment-pill");
    expect(pill).toHaveAttribute("data-error", "true");
    expect(screen.getByText("Larger than 5 MiB").className).toContain("text-destructive");
    // No thumbnail for a rejected image, so it cannot be opened.
    expect(screen.queryByRole("img", { name: "shot.png" })).toBeNull();
  });
});

describe("AttachmentTray", () => {
  it("renders nothing when empty", () => {
    const { container } = render(<AttachmentTray items={[]} onRemove={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("opens the lightbox for an image and closes it with Escape", () => {
    render(<AttachmentTray items={[image, doc]} onRemove={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Preview shot.png" }));
    const box = screen.getByTestId("attachment-lightbox");
    expect(box.querySelector("img")).toHaveAttribute("alt", "shot.png");
    fireEvent.keyDown(box, { key: "Escape" });
    expect(screen.queryByTestId("attachment-lightbox")).toBeNull();
  });

  it("the lightbox close button is a contrasting chip outside the image, and the image is not framed", () => {
    render(<AttachmentTray items={[image]} onRemove={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Preview shot.png" }));
    const box = screen.getByTestId("attachment-lightbox");
    const close = screen.getByTestId("attachment-lightbox-close");
    // Outside the content (so never drawn on the picture) and a dark chip with a light ring: visible on a white image.
    expect(box.contains(close)).toBe(false);
    expect(close.className).toContain("bg-black/70");
    expect(close.className).toContain("text-white");
    expect(box.className).not.toContain("bg-background");
    expect(box.className).not.toContain("border");
    fireEvent.click(close);
    expect(screen.queryByTestId("attachment-lightbox")).toBeNull();
  });

  it("renders a PR/issue pill with #number title, kind and repo, opens its url and removes", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const onRemove = vi.fn();
    const prItem: DraftAttachment = { localId: "p", kind: "pr", forge: "github", number: 123, repo: "o/r", title: "Fix it", url: "https://github.com/o/r/pull/123" };
    render(<AttachmentPill item={prItem} onRemove={onRemove} />);
    expect(screen.getByText("#123 Fix it")).toBeInTheDocument();
    expect(screen.getByText("PR · o/r")).toBeInTheDocument();
    fireEvent.click(screen.getByText("#123 Fix it"));
    expect(open).toHaveBeenCalledWith("https://github.com/o/r/pull/123", "_blank", "noopener,noreferrer");
    fireEvent.click(screen.getByRole("button", { name: "Remove #123 Fix it" }));
    expect(onRemove).toHaveBeenCalled();
    cleanup();
    render(<AttachmentPill item={{ ...prItem, kind: "issue" }} onRemove={onRemove} />);
    expect(screen.getByText("Issue · o/r")).toBeInTheDocument();
    open.mockRestore();
  });
});
