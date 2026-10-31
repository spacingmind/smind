import { readFileSync } from "node:fs";
import path from "node:path";
import { act } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
  desktop: false,
  openPath: vi.fn(() => Promise.resolve()),
  local: null as null | { revealLabel: string; editors: never[]; reveal: (p: string) => void; open: () => void },
}));
vi.mock("@/lib/platform", async (orig) => ({
  ...(await orig<typeof import("@/lib/platform")>()),
  get isDesktop() {
    return h.desktop;
  },
  desktop: { openPathDefault: h.openPath },
}));
vi.mock("@/hooks/use-local-path-actions", () => ({ useLocalPathActions: () => h.local }));

import { DESKTOP_OPENABLE_EXTENSIONS, FileActionButtons, absolutePathOf, availableFileActions } from "@/components/file-actions";
import { FileEditorPane } from "@/components/file-editor-pane";
import type { FileStatResult, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";

const TASK: Task = {
  ID: 3, WorkspaceID: 1, SpaceID: null, Title: "t", Status: "active", WorktreePath: "/home/u/wt", Branch: "b",
  CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null,
};

const reveal = vi.fn();
const LOCAL = { revealLabel: "Reveal in Finder", editors: [] as never[], reveal, open: vi.fn() };

function b64(n: number): string {
  return btoa("x".repeat(n));
}

beforeEach(() => {
  h.desktop = false;
  h.local = null;
  h.openPath.mockClear();
  reveal.mockClear();
});

describe("which actions a file offers", () => {
  it("availableFileActions", () => {
    const web = (ext: string) => availableFileActions({ desktop: false, local: false, ext });
    const local = (ext: string) => availableFileActions({ desktop: true, local: true, ext });
    const remote = (ext: string) => availableFileActions({ desktop: true, local: false, ext });
    expect(web("pdf")).toEqual(["download", "copy-path"]);
    expect(web("docx")).toEqual(["download", "copy-path"]);
    expect(local("pdf")).toEqual(["open-in-app", "reveal", "copy-path"]);
    expect(local("DOCX")).toEqual(["open-in-app", "reveal", "copy-path"]);
    expect(local("html")).toEqual(["reveal", "copy-path"]); // html is not on the desktop opener list
    expect(local("svg")).toEqual(["reveal", "copy-path"]);
    expect(remote("pdf")).toEqual(["copy-path"]); // a remote daemon's files are not on this machine
    // Download is the web's; the desktop never offers it
    for (const ext of ["pdf", "docx", "md"]) expect(local(ext)).not.toContain("download");
    // a web build never offers the desktop-only actions, even when `local` were true
    expect(availableFileActions({ desktop: false, local: true, ext: "pdf" })).toEqual(["download", "copy-path"]);
  });

  it("desktop_openable_list_matches_rust", () => {
    // vitest runs with web/packages/ui as cwd
    const rust = readFileSync(path.resolve(process.cwd(), "../../../desktop/src-tauri/src/open_path.rs"), "utf8");
    const block = /ALLOWED_EXTENSIONS: \[&str; (\d+)\] = \[([^\]]*)\]/.exec(rust);
    expect(block, "ALLOWED_EXTENSIONS not found in open_path.rs").not.toBeNull();
    const fromRust = [...block![2].matchAll(/"([a-z0-9]+)"/g)].map((m) => m[1]);
    expect(fromRust).toHaveLength(Number(block![1]));
    expect([...DESKTOP_OPENABLE_EXTENSIONS]).toEqual(fromRust); // same list, same order: the UI never offers what Rust refuses
    for (const never of ["exe", "app", "command", "bat", "cmd", "ps1", "sh", "msi", "dmg", "jar", "lnk", "url", "desktop", "scr", "html", "svg"]) {
      expect(DESKTOP_OPENABLE_EXTENSIONS).not.toContain(never);
    }
  });

  it("absolutePathOf", () => {
    expect(absolutePathOf("/home/u/wt", "out/a.pdf")).toBe("/home/u/wt/out/a.pdf");
    expect(absolutePathOf("/home/u/wt/", "a.pdf")).toBe("/home/u/wt/a.pdf");
    expect(absolutePathOf(String.raw`C:\Users\u\wt`, "out/a.pdf")).toBe(String.raw`C:\Users\u\wt\out\a.pdf`);
    expect(absolutePathOf(String.raw`C:\Users\u\wt\\`, "a.pdf")).toBe(String.raw`C:\Users\u\wt\a.pdf`);
    expect(absolutePathOf(undefined, "out/a.pdf")).toBe("out/a.pdf");
  });
});

describe("the action buttons", () => {
  const target = (client: FakeWsClient, over: Record<string, unknown> = {}) => ({
    client, taskId: 3, worktreePath: "/home/u/wt", path: "out/a.pdf", ext: "pdf", size: 6 * 1024 * 1024, mtime: "T0", ...over,
  });

  it("download shows progress above 5 MiB and saves under the basename", async () => {
    const create = vi.fn(() => "blob:x");
    const revoke = vi.fn();
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    const client = new FakeWsClient();
    render(<FileActionButtons target={target(client)} pathActions={null} />);
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    await waitFor(() => expect(client.calls).toHaveLength(1));
    expect((client.nth("file.readRange", 0).params as Record<string, unknown>).ifMtime).toBe("T0");
    await act(async () => client.nth("file.readRange", 0).resolve({ offset: 0, length: 3 * 1024 * 1024, eof: false, data: b64(3 * 1024 * 1024), mtime: "T0" }));
    expect(await screen.findByRole("button", { name: "Downloading 50%" })).toBeDisabled();
    await waitFor(() => expect(client.calls).toHaveLength(2));
    await act(async () => client.nth("file.readRange", 1).resolve({ offset: 3 * 1024 * 1024, length: 3 * 1024 * 1024, eof: true, data: b64(3 * 1024 * 1024), mtime: "T0" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Download" })).toBeEnabled());
    expect(click).toHaveBeenCalledTimes(1);
    expect(revoke).toHaveBeenCalledTimes(1);
    click.mockRestore();
  });

  it("a small download shows no percentage", async () => {
    Object.assign(URL, { createObjectURL: () => "blob:y", revokeObjectURL: () => undefined });
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    const client = new FakeWsClient();
    render(<FileActionButtons target={target(client, { size: 10 })} pathActions={null} />);
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(await screen.findByRole("button", { name: "Downloading…" })).toBeInTheDocument();
    await waitFor(() => expect(client.calls).toHaveLength(1));
    await act(async () => client.nth("file.readRange", 0).resolve({ offset: 0, length: 10, eof: true, data: b64(10), mtime: "T0" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Download" })).toBeEnabled());
  });

  it("a file over the 50 MiB cap asks for nothing", async () => {
    const client = new FakeWsClient();
    render(<FileActionButtons target={target(client, { size: 51 * 1024 * 1024 })} pathActions={null} />);
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(client.calls).toHaveLength(0);
    expect(screen.getByRole("button", { name: "Download" })).toBeEnabled();
  });

  it("copy path writes the absolute path", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.assign(navigator, { clipboard: { writeText } });
    render(<FileActionButtons target={target(new FakeWsClient())} pathActions={null} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy path" }));
    expect(writeText).toHaveBeenCalledWith("/home/u/wt/out/a.pdf");
  });

  it("open in app passes the root and the relative path, nothing else", () => {
    h.desktop = true;
    render(<FileActionButtons target={target(new FakeWsClient(), { ext: "docx", path: "out/b.docx" })} pathActions={LOCAL} />);
    fireEvent.click(screen.getByRole("button", { name: "Open in app" }));
    expect(h.openPath).toHaveBeenCalledTimes(1);
    expect(h.openPath).toHaveBeenCalledWith("/home/u/wt", "out/b.docx");
    fireEvent.click(screen.getByRole("button", { name: "Reveal in Finder" }));
    expect(reveal).toHaveBeenCalledWith("/home/u/wt/out/b.docx");
  });
});

describe("the No-preview panel", () => {
  const officeStat: FileStatResult = { size: 2048, mtime: "T0", kind: "office", mime: "application/zip", ext: ".docx", mismatch: false, previewable: false };

  async function openMemo(): Promise<FakeWsClient> {
    const client = new FakeWsClient();
    render(<FileEditorPane client={client} task={TASK} path="memo.docx" />);
    await waitFor(() => client.nth("file.stat", 0));
    await act(async () => client.nth("file.stat", 0).resolve(officeStat));
    await screen.findByTestId("no-preview-panel");
    return client;
  }

  it("gains Download and Copy path on the web", async () => {
    await openMemo();
    const actions = await screen.findByTestId("no-preview-actions");
    expect(within(actions).getAllByRole("button").map((b) => b.textContent)).toEqual(["Download", "Copy path"]);
  });

  it("gains Open in app, Reveal and Copy path on a desktop attached to the local daemon", async () => {
    h.desktop = true;
    h.local = LOCAL;
    await openMemo();
    const actions = await screen.findByTestId("no-preview-actions");
    expect(within(actions).getAllByRole("button").map((b) => b.textContent)).toEqual(["Open in app", "Reveal in Finder", "Copy path"]);
    fireEvent.click(within(actions).getByRole("button", { name: "Open in app" }));
    expect(h.openPath).toHaveBeenCalledWith("/home/u/wt", "memo.docx");
  });

  it("offers only Copy path on a desktop attached to a remote daemon", async () => {
    h.desktop = true;
    h.local = null;
    await openMemo();
    const actions = await screen.findByTestId("no-preview-actions");
    expect(within(actions).getAllByRole("button").map((b) => b.textContent)).toEqual(["Copy path"]);
  });
});
