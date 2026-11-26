// UI cleanup B1 (docs/plans/active/ui-cleanup-b1.md H2): the redundant visual
// shortcut surfaces are gone while the shortcuts behind them keep working.
// The composer is mounted the way composer.test.tsx's renderComposer does
// (a WsClient prop under the daemon query provider), so no new harness here.
import { fireEvent, screen } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { beforeEach, describe, expect, it } from "vitest";

import { Composer } from "@/components/composer/composer";
import { FakeWsClient } from "@/test/fake-ws-client";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";

function composerProps(): React.ComponentProps<typeof Composer> {
  return {
    client: new FakeWsClient(),
    taskId: 1,
    chatId: 10,
    isDefaultChat: true,
    boundProvider: null,
    connected: true,
    runningRunId: null,
    queuedItems: [],
    onCancelQueued: async () => {},
    onSubmit: async () => ({ kind: "started", runId: "r" }),
    onStop: async () => {},
  };
}

function textarea(): HTMLTextAreaElement {
  return screen.getByLabelText("Prompt") as HTMLTextAreaElement;
}

describe("UI cleanup B1", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("ui_cleanup_composer_has_no_focus_hint", () => {
    render(<Composer {...composerProps()} />);
    expect(textarea()).toHaveValue("");
    expect(textarea()).not.toHaveFocus();
    expect(screen.queryByTestId("composer-focus-hint")).not.toBeInTheDocument();
    expect(screen.queryByText(/to focus/)).not.toBeInTheDocument();
  });

  it("ui_cleanup_composer_focus_keystroke_still_focuses", () => {
    render(
      <KeyboardProvider>
        <Composer {...composerProps()} />
      </KeyboardProvider>,
    );
    expect(textarea()).not.toHaveFocus();
    // Ctrl+L: jsdom is not a mac, so Mod resolves to Ctrl.
    fireEvent.keyDown(document, { key: "l", code: "KeyL", ctrlKey: true });
    expect(textarea()).toHaveFocus();
  });
});
