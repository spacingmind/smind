import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  KeyboardProvider,
  useActionHandler,
  useKeyboard,
  useModalKeyboardLock,
} from "@/keyboard/keyboard-provider";
import type { ActionId } from "@/keyboard/actions";

/** Ctrl-based combos throughout: `isMacPlatform()` reads jsdom's navigator, which is not a mac. */
function pressCtrl(key: string, code: string, extra: Partial<KeyboardEventInit> = {}) {
  fireEvent.keyDown(document, { key, code, ctrlKey: true, ...extra });
}

function Claim({
  action,
  onFire,
  enabled = true,
  label,
}: {
  action: ActionId;
  onFire: (payload: unknown) => void;
  enabled?: boolean;
  label?: string;
}) {
  useActionHandler(action, onFire, { enabled });
  return label === undefined ? null : <span>{label}</span>;
}

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("KeyboardProvider dispatch", () => {
  it("routes a matching key event to the action's handler", () => {
    const fire = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
      </KeyboardProvider>,
    );

    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(1);
    expect(fire).toHaveBeenCalledWith(null);
  });

  it("does not fire for a near-miss modifier", () => {
    const fire = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
      </KeyboardProvider>,
    );

    pressCtrl("k", "KeyK", { shiftKey: true });
    fireEvent.keyDown(document, { key: "k", code: "KeyK" });
    fireEvent.keyDown(document, { key: "k", code: "KeyK", metaKey: true });
    expect(fire).not.toHaveBeenCalled();
  });

  it("hands a Digit-wildcard binding the digit that was pressed", () => {
    const fire = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="tab.jump" onFire={fire} />
      </KeyboardProvider>,
    );

    fireEvent.keyDown(document, { key: "3", code: "Digit3", ctrlKey: true, altKey: true });
    expect(fire).toHaveBeenCalledWith({ digit: 3 });
  });

  it("does not fire a non-global binding while focus is in a text input", () => {
    const help = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="shortcuts.help" onFire={help} />
        <input aria-label="field" />
      </KeyboardProvider>,
    );

    const input = screen.getByLabelText("field");
    input.focus();
    fireEvent.keyDown(input, { key: "?", code: "Slash", shiftKey: true });
    expect(help).not.toHaveBeenCalled();

    input.blur();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    expect(help).toHaveBeenCalledTimes(1);
  });

  it("does not fire a non-global binding inside a textarea or an editor surface", () => {
    const help = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="shortcuts.help" onFire={help} />
        <textarea aria-label="composer" />
        <div className="cm-editor">
          <div aria-label="code" tabIndex={0} />
        </div>
      </KeyboardProvider>,
    );

    for (const label of ["composer", "code"]) {
      const el = screen.getByLabelText(label);
      el.focus();
      fireEvent.keyDown(el, { key: "?", code: "Slash", shiftKey: true });
      el.blur();
    }
    expect(help).not.toHaveBeenCalled();
  });

  it("fires a global binding from inside a text input", () => {
    const fire = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
        <input aria-label="field" />
      </KeyboardProvider>,
    );

    const input = screen.getByLabelText("field");
    input.focus();
    fireEvent.keyDown(input, { key: "k", code: "KeyK", ctrlKey: true });
    expect(fire).toHaveBeenCalledTimes(1);
  });

  it("fires nothing while a modal holds the keyboard lock", () => {
    const fire = vi.fn();
    function Modal() {
      useModalKeyboardLock(true);
      return null;
    }
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
        <Modal />
      </KeyboardProvider>,
    );

    pressCtrl("k", "KeyK");
    expect(fire).not.toHaveBeenCalled();
  });

  it("releases the lock only once every holder is gone", () => {
    const fire = vi.fn();
    function Lock() {
      useModalKeyboardLock(true);
      return null;
    }
    function Host() {
      const [locks, setLocks] = useState(2);
      return (
        <>
          <Claim action="palette.open" onFire={fire} />
          {Array.from({ length: locks }, (_, i) => (
            <Lock key={i} />
          ))}
          <button onClick={() => setLocks((n) => n - 1)}>release</button>
        </>
      );
    }
    render(
      <KeyboardProvider>
        <Host />
      </KeyboardProvider>,
    );

    fireEvent.click(screen.getByText("release"));
    pressCtrl("k", "KeyK");
    expect(fire).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("release"));
    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(1);
  });

  it("ignores auto-repeat", () => {
    const fire = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
      </KeyboardProvider>,
    );
    pressCtrl("k", "KeyK", { repeat: true });
    expect(fire).not.toHaveBeenCalled();
  });
});

describe("handler registry", () => {
  it("dispatches to the most recently registered enabled handler", () => {
    const outer = vi.fn();
    const inner = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={outer} />
        <Claim action="palette.open" onFire={inner} />
      </KeyboardProvider>,
    );

    pressCtrl("k", "KeyK");
    expect(inner).toHaveBeenCalledTimes(1);
    expect(outer).not.toHaveBeenCalled();
  });

  it("skips a disabled handler and falls through to the next one", () => {
    const outer = vi.fn();
    const inner = vi.fn();
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={outer} />
        <Claim action="palette.open" onFire={inner} enabled={false} />
      </KeyboardProvider>,
    );

    pressCtrl("k", "KeyK");
    expect(outer).toHaveBeenCalledTimes(1);
    expect(inner).not.toHaveBeenCalled();
  });

  it("unregisters on unmount", () => {
    const fire = vi.fn();
    function Host() {
      const [mounted, setMounted] = useState(true);
      return (
        <>
          {mounted && <Claim action="palette.open" onFire={fire} />}
          <button onClick={() => setMounted(false)}>unmount</button>
        </>
      );
    }
    render(
      <KeyboardProvider>
        <Host />
      </KeyboardProvider>,
    );

    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByText("unmount"));
    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(1);
  });

  it("leaves the browser default alone when nothing claims the action", () => {
    render(
      <KeyboardProvider>
        <div />
      </KeyboardProvider>,
    );
    // Ctrl+W with no `tab.close` handler must not be swallowed.
    const event = new KeyboardEvent("keydown", {
      key: "w",
      code: "KeyW",
      ctrlKey: true,
      cancelable: true,
      bubbles: true,
    });
    document.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });

  it("preventDefaults an event a handler took", () => {
    render(
      <KeyboardProvider>
        <Claim action="tab.close" onFire={() => {}} />
      </KeyboardProvider>,
    );
    const event = new KeyboardEvent("keydown", {
      key: "w",
      code: "KeyW",
      ctrlKey: true,
      cancelable: true,
      bubbles: true,
    });
    document.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
  });

  it("runs an action directly, with no key involved -- the palette's path", () => {
    const fire = vi.fn();
    function Runner() {
      const { runAction } = useKeyboard();
      return <button onClick={() => runAction("palette.open")}>run</button>;
    }
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
        <Runner />
      </KeyboardProvider>,
    );

    fireEvent.click(screen.getByText("run"));
    expect(fire).toHaveBeenCalledTimes(1);
  });

  it("no-ops outside a provider instead of throwing", () => {
    const fire = vi.fn();
    expect(() => render(<Claim action="palette.open" onFire={fire} label="bare" />)).not.toThrow();
    expect(screen.getByText("bare")).toBeInTheDocument();
    pressCtrl("k", "KeyK");
    expect(fire).not.toHaveBeenCalled();
  });
});

describe("rebinding", () => {
  it("applies an override, persists it, and restores it on remount", () => {
    const fire = vi.fn();
    function Rebinder() {
      const { rebind } = useKeyboard();
      return <button onClick={() => rebind("palette-open", "Ctrl+Shift+P")}>rebind</button>;
    }
    const tree = (
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
        <Rebinder />
      </KeyboardProvider>
    );

    const first = render(tree);
    fireEvent.click(screen.getByText("rebind"));

    pressCtrl("k", "KeyK");
    expect(fire).not.toHaveBeenCalled();
    pressCtrl("p", "KeyP", { shiftKey: true });
    expect(fire).toHaveBeenCalledTimes(1);

    // A real reload: a fresh provider reading the same localStorage.
    first.unmount();
    render(tree);
    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(1);
    pressCtrl("p", "KeyP", { shiftKey: true });
    expect(fire).toHaveBeenCalledTimes(2);
  });

  it("resets a single binding and all bindings back to their defaults", () => {
    const fire = vi.fn();
    function Controls() {
      const { rebind, resetBinding, resetAllBindings } = useKeyboard();
      return (
        <>
          <button onClick={() => rebind("palette-open", "Ctrl+Shift+P")}>rebind</button>
          <button onClick={() => resetBinding("palette-open")}>reset one</button>
          <button onClick={resetAllBindings}>reset all</button>
        </>
      );
    }
    render(
      <KeyboardProvider>
        <Claim action="palette.open" onFire={fire} />
        <Controls />
      </KeyboardProvider>,
    );

    fireEvent.click(screen.getByText("rebind"));
    fireEvent.click(screen.getByText("reset one"));
    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByText("rebind"));
    fireEvent.click(screen.getByText("reset all"));
    pressCtrl("k", "KeyK");
    expect(fire).toHaveBeenCalledTimes(2);
  });
});
