import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FolderPickerDialog } from "@/components/folder-picker-dialog";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { FsListDirResult } from "@/lib/types";

const HOME_RESULT: FsListDirResult = {
  path: "/home/dev",
  parent: "/home",
  entries: [
    { name: "plain", path: "/home/dev/plain", isGitRepo: false },
    { name: "repo", path: "/home/dev/repo", isGitRepo: true },
  ],
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderDialog(client: FakeWsClient, onSelect = vi.fn(), onOpenChange = vi.fn()) {
  render(<FolderPickerDialog client={client as never} open onOpenChange={onOpenChange} onSelect={onSelect} />);
  return { onSelect, onOpenChange };
}

describe("FolderPickerDialog", () => {
  it("opening calls fs.listDir with no path and renders the returned entries", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    const call = await waitFor(() => client.nth("fs.listDir"));
    expect(call.params).toBeUndefined();

    await act(async () => {
      call.resolve(HOME_RESULT);
    });
    await flush();

    expect(await screen.findByText("plain")).toBeInTheDocument();
    expect(screen.getByText("repo")).toBeInTheDocument();
    expect(screen.getByTitle("/home/dev")).toBeInTheDocument();
  });

  it("clicking a row re-queries fs.listDir with that row's path and updates the list", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    const first = await waitFor(() => client.nth("fs.listDir", 0));
    await act(async () => {
      first.resolve(HOME_RESULT);
    });
    await flush();

    fireEvent.click(await screen.findByText("repo"));

    const second = await waitFor(() => client.nth("fs.listDir", 1));
    expect(second.params).toEqual({ path: "/home/dev/repo" });

    await act(async () => {
      second.resolve({
        path: "/home/dev/repo",
        parent: "/home/dev",
        entries: [{ name: "nested", path: "/home/dev/repo/nested", isGitRepo: false }],
      });
    });
    await flush();

    expect(await screen.findByText("nested")).toBeInTheDocument();
    expect(screen.queryByText("plain")).not.toBeInTheDocument();
  });

  it("Up queries the parent path; disabled when the parent is empty (filesystem root)", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    const first = await waitFor(() => client.nth("fs.listDir", 0));
    await act(async () => {
      first.resolve(HOME_RESULT);
    });
    await flush();

    const up = await screen.findByRole("button", { name: "Up" });
    expect(up).not.toBeDisabled();
    fireEvent.click(up);

    const second = await waitFor(() => client.nth("fs.listDir", 1));
    expect(second.params).toEqual({ path: "/home" });

    await act(async () => {
      second.resolve({ path: "/home", parent: "", entries: [] });
    });
    await flush();

    await waitFor(() => expect(screen.getByRole("button", { name: "Up" })).toBeDisabled());
  });

  it("shows a visible indicator on a git-repo entry and not on a plain one", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    const first = await waitFor(() => client.nth("fs.listDir", 0));
    await act(async () => {
      first.resolve(HOME_RESULT);
    });
    await flush();

    const plainRow = (await screen.findByText("plain")).closest("[data-testid='folder-row']");
    const repoRow = (await screen.findByText("repo")).closest("[data-testid='folder-row']");
    expect(repoRow?.querySelector("[data-testid='git-repo-indicator']")).not.toBeNull();
    expect(plainRow?.querySelector("[data-testid='git-repo-indicator']")).toBeNull();
  });

  it("Use this folder calls onSelect with the current path and closes, without calling workspace.create", async () => {
    const client = new FakeWsClient();
    const { onSelect, onOpenChange } = renderDialog(client);

    const first = await waitFor(() => client.nth("fs.listDir", 0));
    await act(async () => {
      first.resolve(HOME_RESULT);
    });
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "Use this folder" }));

    expect(onSelect).toHaveBeenCalledWith("/home/dev");
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(client.calls.some((c) => c.method === "workspace.create")).toBe(false);
  });

  it("an fs.listDir error while navigating shows inline and leaves the previous listing visible", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    const first = await waitFor(() => client.nth("fs.listDir", 0));
    await act(async () => {
      first.resolve(HOME_RESULT);
    });
    await flush();

    fireEvent.click(await screen.findByText("repo"));

    const second = await waitFor(() => client.nth("fs.listDir", 1));
    await act(async () => {
      second.reject(new Error("fs.listDir: permission denied"));
    });
    await flush();

    expect(await screen.findByText(/permission denied/)).toBeInTheDocument();
    // The previous listing (home dir's entries) is still visible, not blanked.
    expect(screen.getByText("plain")).toBeInTheDocument();
    expect(screen.getByText("repo")).toBeInTheDocument();
  });
});
