import { act, StrictMode } from "react";
import { screen } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { beforeEach, describe, expect, it } from "vitest";

import { Composer } from "@/components/composer/composer";
import { draftStorageKey } from "@/components/composer/use-composer-draft";
import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { FakeWsClient } from "@/test/fake-ws-client";
import { usePendingSendStore } from "@/stores/pending-send-store";

interface Submission {
  provider: string;
  prompt: string;
  permissionMode: string;
  autoAccept: boolean;
}

const CONFIG: RunConfigState = {
  baseAgentId: null,
  custom: true,
  provider: "claude-native",
  permissionMode: "plan",
  autoAccept: false,
  thinkingLevel: "",
  model: "",
};

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  });
}

function textarea(): HTMLTextAreaElement {
  return screen.getByLabelText("Prompt") as HTMLTextAreaElement;
}

async function renderComposer(opts: { strict?: boolean; fail?: boolean; isDefaultChat?: boolean } = {}) {
  const submissions: Submission[] = [];
  const ui = (
    <Composer
      client={new FakeWsClient()}
      taskId={1}
      chatId={10}
      isDefaultChat={opts.isDefaultChat ?? true}
      boundProvider={null}
      connected
      runningRunId={null}
      queuedItems={[]}
      onCancelQueued={async () => {}}
      onSubmit={async (provider, prompt, permission) => {
        if (opts.fail) throw new Error("boom");
        submissions.push({ provider, prompt, ...permission });
        return { kind: "started", runId: "run-test" };
      }}
      onStop={async () => {}}
    />
  );
  render(opts.strict ? <StrictMode>{ui}</StrictMode> : ui);
  await flush();
  await flush();
  return { submissions };
}

describe("Composer pending send", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.localStorage.setItem(draftStorageKey(1), "hello");
    usePendingSendStore.setState({ entry: null });
    usePendingSendStore.getState().set(1, CONFIG);
  });

  it("pending send applies the config then submits once", async () => {
    const { submissions } = await renderComposer();
    expect(submissions).toEqual([{ provider: "claude-native", prompt: "hello", permissionMode: "plan", autoAccept: false }]);
    expect(usePendingSendStore.getState().entry).toBeNull();
  });

  it("no double send under StrictMode", async () => {
    const { submissions } = await renderComposer({ strict: true });
    expect(submissions).toHaveLength(1);
  });

  it("send failure keeps the draft", async () => {
    await renderComposer({ fail: true });
    expect(textarea().value).toBe("hello");
    expect(screen.getByRole("alert")).toBeTruthy();
  });

  it("non-default chat does not take it", async () => {
    const { submissions } = await renderComposer({ isDefaultChat: false });
    expect(submissions).toEqual([]);
    expect(usePendingSendStore.getState().entry).not.toBeNull();
  });
});
