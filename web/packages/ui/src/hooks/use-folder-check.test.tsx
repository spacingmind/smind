import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useFolderCheck, type FolderStatus } from "@/hooks/use-folder-check";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { FsListDirResult, Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

function workspace(path: string, title: string): Workspace {
  return {
    ID: 1,
    Path: path,
    Title: title,
    RoutingPolicy: "",
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
  };
}

function listing(path: string, entries: Array<{ name: string; path: string; isGitRepo: boolean }> = []): FsListDirResult {
  return { path, parent: path.slice(0, path.lastIndexOf("/")) || "/", entries };
}

/** Renders the hook with a fresh fake client; the check promise and the client's recorded calls are driven by hand. */
function mountCheck(workspaces: Workspace[] = []) {
  const client = new FakeWsClient();
  const view = renderHook(
    (props: { client: WsClient | null; workspaces: Workspace[] }) => useFolderCheck(props.client, props.workspaces),
    { initialProps: { client: client as unknown as WsClient, workspaces } },
  );
  return { view, client };
}

/** Starts a check, answers the single fs.listDir it makes (if any), and returns the status it settled on. */
async function runCheck(
  client: FakeWsClient,
  check: (raw: string) => Promise<FolderStatus>,
  raw: string,
  answer: FsListDirResult | Error,
): Promise<FolderStatus> {
  let promise!: Promise<FolderStatus>;
  act(() => {
    promise = check(raw);
  });
  const fsCalls = client.calls.filter((call) => call.method === "fs.listDir");
  if (fsCalls.length > 0) {
    await act(async () => {
      if (answer instanceof Error) fsCalls[0]!.reject(answer);
      else fsCalls[0]!.resolve(answer);
    });
  }
  return act(async () => await promise);
}

describe("useFolderCheck", () => {
  it("folder_check_judges_a_typed_path_from_the_parents_listing", async () => {
    const cases: Array<{
      what: string;
      raw: string;
      workspaces?: Workspace[];
      answer?: FsListDirResult | Error;
      want: FolderStatus;
      /** The exact fs.listDir params the check must send, in order (empty: no call at all). */
      listDirParams: unknown[];
    }> = [
      {
        what: "nothing typed",
        raw: "   ",
        want: { kind: "empty", path: "" },
        listDirParams: [],
      },
      {
        what: "a relative path",
        raw: "code/x",
        want: { kind: "relative", path: "code/x" },
        listDirParams: [],
      },
      {
        what: "a git repository",
        raw: "/w/beta",
        answer: listing("/w", [{ name: "beta", path: "/w/beta", isGitRepo: true }]),
        want: { kind: "git", path: "/w/beta" },
        listDirParams: [{ path: "/w" }],
      },
      {
        what: "a plain folder",
        raw: "/w/beta",
        answer: listing("/w", [{ name: "beta", path: "/w/beta", isGitRepo: false }]),
        want: { kind: "not-git", path: "/w/beta" },
        listDirParams: [{ path: "/w" }],
      },
      {
        what: "not in the parent's listing",
        raw: "/w/beta",
        answer: listing("/w", [{ name: "other", path: "/w/other", isGitRepo: true }]),
        want: { kind: "missing", path: "/w/beta" },
        listDirParams: [{ path: "/w" }],
      },
      {
        what: "a hidden last segment the listing cannot show",
        raw: "/w/.config",
        answer: listing("/w"),
        want: { kind: "unknown", path: "/w/.config" },
        listDirParams: [{ path: "/w" }],
      },
      {
        what: "a listing that refuses, saying the folder is not there",
        raw: "/w/beta",
        answer: new Error("fs.listDir: open /w: no such file or directory"),
        want: { kind: "missing", path: "/w/beta" },
        listDirParams: [{ path: "/w" }],
      },
      {
        what: "a listing that fails any other way",
        raw: "/w/beta",
        answer: new Error("fs.listDir: /w: permission denied"),
        want: { kind: "unknown", path: "/w/beta" },
        listDirParams: [{ path: "/w" }],
      },
    ];

    for (const c of cases) {
      const { view, client } = mountCheck(c.workspaces);
      const settled = await runCheck(client, view.result.current.check, c.raw, c.answer ?? listing("/w"));
      expect(settled, c.what).toEqual(c.want);
      expect(view.result.current.status, c.what).toEqual(c.want);
      expect(
        client.calls.filter((call) => call.method === "fs.listDir").map((call) => call.params),
        c.what,
      ).toEqual(c.listDirParams);
    }
  });

  it("folder_check_a_duplicate_path_is_decided_without_any_fs_call", async () => {
    const alpha = workspace("/w/alpha", "Alpha");
    const { view, client } = mountCheck([alpha]);

    let settled!: FolderStatus;
    await act(async () => {
      settled = await view.result.current.check("/w/alpha/");
    });
    // The trailing slash and the case are kept; only whitespace and separator runs are normalised away.
    expect(settled).toEqual({ kind: "duplicate", workspace: alpha, path: "/w/alpha" });
    expect(view.result.current.status).toEqual({ kind: "duplicate", workspace: alpha, path: "/w/alpha" });
    expect(client.calls).toHaveLength(0);
  });

  it("folder_check_tilde_is_expanded_with_the_home_fetched_once", async () => {
    const { view, client } = mountCheck();

    let first!: Promise<FolderStatus>;
    act(() => {
      first = view.result.current.check("~/code/x");
    });
    const homeCall = client.nth("fs.listDir", 0);
    expect(homeCall.params).toEqual({});
    await act(async () => {
      homeCall.resolve(listing("/home/u"));
    });
    // The expansion happened before the listing: the parent of /home/u/code/x is asked, not of ~/code/x.
    const parentCall = client.nth("fs.listDir", 1);
    expect(parentCall.params).toEqual({ path: "/home/u/code" });
    await act(async () => {
      parentCall.resolve(listing("/home/u/code", [{ name: "x", path: "/home/u/code/x", isGitRepo: true }]));
      await first;
    });
    expect(view.result.current.status).toEqual({ kind: "git", path: "/home/u/code/x" });
    expect(view.result.current.home).toBe("/home/u");

    // The home is cached for the hook's life: a second ~ check lists only the parent.
    let second!: Promise<FolderStatus>;
    act(() => {
      second = view.result.current.check("~/code/y");
    });
    const parentCallAgain = client.nth("fs.listDir", 2);
    expect(parentCallAgain.params).toEqual({ path: "/home/u/code" });
    await act(async () => {
      parentCallAgain.resolve(listing("/home/u/code", [{ name: "x", path: "/home/u/code/x", isGitRepo: true }]));
      await second;
    });
    expect(view.result.current.status).toEqual({ kind: "missing", path: "/home/u/code/y" });
    expect(client.calls.filter((call) => call.method === "fs.listDir")).toHaveLength(3);
  });

  it("folder_check_a_home_fetch_that_fails_is_unknown_and_stops_there", async () => {
    const { view, client } = mountCheck();

    let settled!: FolderStatus;
    await act(async () => {
      const homeCall = view.result.current.check("~/x");
      client.nth("fs.listDir", 0).reject(new Error("connection closed"));
      settled = await homeCall;
    });
    expect(settled).toEqual({ kind: "unknown", path: "~/x" });
    // No parent listing was attempted after the home refused.
    expect(client.calls.filter((call) => call.method === "fs.listDir")).toHaveLength(1);
  });

  it("folder_check_a_stale_answer_cannot_overwrite_the_newer_check", async () => {
    const { view, client } = mountCheck();

    let first!: Promise<FolderStatus>;
    act(() => {
      first = view.result.current.check("/a/b");
    });
    let second!: Promise<FolderStatus>;
    act(() => {
      second = view.result.current.check("/c/d");
    });
    const slow = client.nth("fs.listDir", 0);
    const fast = client.nth("fs.listDir", 1);

    await act(async () => {
      fast.resolve(listing("/c", [{ name: "d", path: "/c/d", isGitRepo: true }]));
      await second;
    });
    expect(view.result.current.status).toEqual({ kind: "git", path: "/c/d" });

    // The slower first answer lands last and must not overwrite the newer check's.
    await act(async () => {
      slow.resolve(listing("/a", [{ name: "b", path: "/a/b", isGitRepo: false }]));
      await first;
    });
    expect(view.result.current.status).toEqual({ kind: "git", path: "/c/d" });
  });
});
