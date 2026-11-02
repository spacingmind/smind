import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ReactElement, ReactNode } from "react";

import { HeredocReveal } from "@/components/permission/heredoc-reveal";
import { OptionsCard } from "@/components/permission/options-card";
import { PermissionDetailView } from "@/components/permission/permission-detail";
import { ToolCallDetail } from "@/components/timeline/tool-call-detail";
import type { PendingPermission, TimelineToolCallItem } from "@/hooks/use-run-timeline";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import type { HeredocReveal as HeredocRevealResult, HeredocRef, PermissionDetail } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";

function revealResult(overrides: Partial<HeredocRevealResult> = {}): HeredocRevealResult {
  return { index: 0, bytes: 12, text: "TOKEN=[redacted]", redacted: false, truncated: false, ...overrides };
}

/** Renders ui inside a DaemonQueryProvider whose connection is a FakeWsClient, so reveal RPCs get recorded and are resolved by hand. */
function renderWithClient(ui: ReactElement): { client: FakeWsClient } {
  const client = new FakeWsClient();
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <DaemonQueryProvider queryClient={createQueryClient()} client={client as never} events={null}>
        {children}
      </DaemonQueryProvider>
    );
  }
  render(ui, { wrapper: Wrapper });
  return { client };
}

describe("heredoc reveal", () => {
  it("HeredocReveal: collapsed by default, fetches once on first open, caches on reopen", async () => {
    const refs: HeredocRef[] = [{ index: 0, bytes: 12 }];
    const reveal = vi.fn().mockResolvedValue(revealResult({ text: "the body" }));
    render(<HeredocReveal refs={refs} reveal={reveal} testId="permission-heredoc" />);

    expect(screen.queryByTestId("permission-heredoc-body-0")).toBeNull();
    expect(screen.getByTestId("permission-heredoc-toggle-0")).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByTestId("permission-heredoc-toggle-0")).toHaveTextContent("Show heredoc (12 bytes)");

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    expect(await screen.findByTestId("permission-heredoc-body-0")).toHaveTextContent("the body");
    expect(reveal).toHaveBeenCalledTimes(1);
    expect(reveal).toHaveBeenCalledWith(0);
    expect(screen.getByTestId("permission-heredoc-toggle-0")).toHaveTextContent("Hide heredoc (12 bytes)");

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    expect(screen.queryByTestId("permission-heredoc-body-0")).toBeNull();
    expect(screen.getByTestId("permission-heredoc-toggle-0")).toHaveTextContent("Show heredoc (12 bytes)");

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    expect(screen.getByTestId("permission-heredoc-body-0")).toHaveTextContent("the body");
    // Cached: reopening never asks the daemon again.
    expect(reveal).toHaveBeenCalledTimes(1);
  });

  it("HeredocReveal: shows an error with Retry and retries", async () => {
    const reveal = vi.fn().mockRejectedValueOnce(new Error("not_found")).mockResolvedValue(revealResult({ text: "later" }));
    render(<HeredocReveal refs={[{ index: 0, bytes: 5 }]} reveal={reveal} testId="permission-heredoc" />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    const error = await screen.findByText("not_found");
    expect(error.parentElement?.className).toContain("text-destructive");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    });
    expect(await screen.findByTestId("permission-heredoc-body-0")).toHaveTextContent("later");
    expect(reveal).toHaveBeenCalledTimes(2);
  });

  it("HeredocReveal: renders redacted and truncated notes", async () => {
    const reveal = vi.fn().mockResolvedValue(revealResult({ text: "x", bytes: 70000, redacted: true, truncated: true }));
    render(<HeredocReveal refs={[{ index: 0, bytes: 70000 }]} reveal={reveal} testId="permission-heredoc" />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    expect(await screen.findByTestId("permission-heredoc-body-0")).toBeTruthy();
    expect(screen.getByText("secrets redacted")).toBeTruthy();
    expect(screen.getByText("truncated (70000 bytes in total)")).toBeTruthy();
  });

  it("OptionsCard: heredoc toggle calls permission.revealHeredoc with the request id and index", async () => {
    const pending: PendingPermission = {
      requestId: "req-1",
      summary: "Run command",
      options: [
        { id: "allow-1", label: "Allow", kind: "allow_once" },
        { id: "deny-1", label: "Deny", kind: "reject_once" },
      ],
      detail: { kind: "execute", command: "sh <<EOF [heredoc 12 bytes]", heredocs: [{ index: 0, bytes: 12 }] },
    };
    const { client } = renderWithClient(<OptionsCard runId="run-1" pending={pending} onRespond={vi.fn()} />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    const call = client.nth("permission.revealHeredoc");
    expect(call.params).toEqual({ permissionId: "req-1", index: 0 });

    await act(async () => {
      call.resolve(revealResult({ text: "the body" }));
    });
    expect(screen.getByTestId("permission-heredoc-body-0")).toHaveTextContent("the body");
  });

  it("ToolCallDetail: terminal call with heredocs calls run.revealToolHeredoc with run id, tool call id and index", async () => {
    const item: TimelineToolCallItem = {
      kind: "tool_call",
      id: "tool-0",
      toolCallId: "tc-7",
      toolName: "Bash",
      status: "success",
      input: { command: "sh <<EOF [heredoc 5 bytes]" },
      heredocs: [{ index: 0, bytes: 5 }],
      runId: "run-9",
    };
    const { client } = renderWithClient(<ToolCallDetail item={item} intent="terminal" />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-heredoc-toggle-0"));
    });
    const call = client.nth("run.revealToolHeredoc");
    expect(call.params).toEqual({ runId: "run-9", toolCallId: "tc-7", index: 0 });

    await act(async () => {
      call.resolve(revealResult({ index: 0, bytes: 5, text: "the body" }));
    });
    expect(screen.getByTestId("tool-heredoc-body-0")).toHaveTextContent("the body");
  });

  it("PermissionDetailView: the marker command never contains the secret before reveal", async () => {
    const detail: PermissionDetail = {
      kind: "execute",
      command: "cat <<EOF [heredoc 12 bytes]\nEOF",
      heredocs: [{ index: 0, bytes: 12 }],
    };
    const { client } = renderWithClient(<PermissionDetailView detail={detail} requestId="req-7" />);

    expect(screen.getByTestId("permission-detail-command").textContent).toContain("[heredoc 12 bytes]");
    expect(document.body.textContent).not.toContain("SECRET_TOKEN_123");

    await act(async () => {
      fireEvent.click(screen.getByTestId("permission-heredoc-toggle-0"));
    });
    await act(async () => {
      client.nth("permission.revealHeredoc").resolve(revealResult({ text: "TOKEN=SECRET_TOKEN_123" }));
    });
    expect(screen.getByTestId("permission-heredoc-body-0").textContent).toContain("SECRET_TOKEN_123");
  });
});
