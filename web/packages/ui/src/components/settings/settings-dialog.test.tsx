// UI cleanup B3 (peer A): the Settings dialog -- the modal variant of the
// settings screen wrapped in the Dialog primitive. Mounts the dialog from a
// trigger button harness (the app's callers own the open state the same way)
// so focus return and real unmount behaviour are exercised, not just the
// open markup.
import { act, useState } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SettingsDialog } from "@/components/settings/settings-dialog";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { renderWithClientProp } from "@/test/render-with-daemon";

/** Flushes pending effects plus Radix's deferred work: the pointerdown-outside listener registers on a setTimeout(0), and focus restore on close runs on one. */
async function settle(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** The app's wiring, minimized: a caller with a trigger element holds the open state and closes on the dialog's onClose. */
function Harness({ onClose, initialSectionId }: { onClose?: () => void; initialSectionId?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open settings
      </button>
      <SettingsDialog
        open={open}
        onClose={() => {
          setOpen(false);
          onClose?.();
        }}
        client={null}
        events={null}
        initialSectionId={initialSectionId}
        openNonce={0}
      />
    </>
  );
}

async function openDialog(onClose: () => void = vi.fn(), initialSectionId?: string) {
  const view = renderWithClientProp(<Harness onClose={onClose} initialSectionId={initialSectionId} />);
  fireEvent.click(screen.getByRole("button", { name: "Open settings" }));
  await settle();
  return view;
}

/** Radix FocusScope's own tabbable rules (its TreeWalker: tabIndex >= 0, not disabled, not hidden), mirrored so the wrap test presses Tab on the element Radix considers last. */
function tabbablesIn(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>("*")).filter(
    (el) => el.tabIndex >= 0 && !el.hasAttribute("disabled") && !el.hidden && !(el instanceof HTMLInputElement && el.type === "hidden"),
  );
}

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.cssText = "";
});

describe("SettingsDialog", () => {
  it("ui_cleanup_b3_dialog_is_modal_and_traps_focus", async () => {
    renderWithClientProp(<Harness />);
    // Grabbed before opening: once the dialog is open, Radix hides the app
    // behind it from the accessibility tree, so role queries can't see the
    // trigger any more -- which is exactly what this test asserts.
    const trigger = screen.getByRole("button", { name: "Open settings" });
    fireEvent.click(trigger);
    await settle();

    const dialog = screen.getByTestId("settings-dialog");
    expect(screen.getByRole("dialog")).toBe(dialog);

    // Radix modal behaviour: the app behind is hidden from assistive tech
    // and inert to the pointer.
    expect(trigger.parentElement).toHaveAttribute("aria-hidden", "true");
    expect(document.body.style.pointerEvents).toBe("none");

    // Tab from the last tabbable element wraps to the first, inside the dialog.
    const tabbables = tabbablesIn(dialog);
    expect(tabbables.length).toBeGreaterThan(1);
    const last = tabbables[tabbables.length - 1]!;
    last.focus();
    fireEvent.keyDown(last, { key: "Tab" });
    expect(document.activeElement).toBe(tabbables[0]);
  });

  it("ui_cleanup_b3_dialog_has_a_labelled_title", async () => {
    await openDialog();

    expect(screen.getByRole("dialog", { name: "Settings" })).toBeInTheDocument();
  });

  it("ui_cleanup_b3_esc_closes", async () => {
    const onClose = vi.fn();
    await openDialog(onClose);

    fireEvent.keyDown(document, { key: "Escape" });
    await settle();

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
  });

  it("ui_cleanup_b3_backdrop_click_closes", async () => {
    const onClose = vi.fn();
    await openDialog(onClose);

    const overlay = document.querySelector('[data-slot="dialog-overlay"]');
    expect(overlay).not.toBeNull();
    // Radix defers an outside pointerdown's dismissal to the following click.
    fireEvent.pointerDown(overlay!);
    fireEvent.click(overlay!);
    await settle();

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("ui_cleanup_b3_x_button_closes", async () => {
    const onClose = vi.fn();
    await openDialog(onClose);

    fireEvent.click(screen.getByTestId("settings-close-button"));
    await settle();

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
  });

  it("ui_cleanup_b3_focus_starts_on_the_requested_section", async () => {
    const requested = renderWithClientProp(<Harness initialSectionId="shortcuts" />);
    fireEvent.click(screen.getByRole("button", { name: "Open settings" }));
    await settle();
    expect(document.activeElement).toBe(screen.getByTestId("settings-nav-shortcuts"));
    requested.unmount();

    renderWithClientProp(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Open settings" }));
    await settle();
    expect(document.activeElement).toBe(screen.getByTestId("settings-nav-general"));
  });

  it("ui_cleanup_b3_focus_returns_to_the_opener", async () => {
    const onClose = vi.fn();
    renderWithClientProp(<Harness onClose={onClose} />);
    const trigger = screen.getByRole("button", { name: "Open settings" });
    trigger.focus();
    fireEvent.click(trigger);
    await settle();
    expect(screen.getByTestId("settings-dialog")).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(document.activeElement).toBe(trigger));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("ui_cleanup_b3_modal_variant_has_no_back_row_or_window_controls", async () => {
    const view = await openDialog();

    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-variant", "modal");
    expect(screen.queryByTestId("settings-back-button")).not.toBeInTheDocument();
    expect(screen.queryByTestId("desktop-window-controls")).not.toBeInTheDocument();
    expect(screen.getByTestId("settings-sidebar-top")).toHaveTextContent("Settings");
    expect(screen.getByTestId("settings-close-button")).toHaveAccessibleName("Close settings");
    view.unmount();

    // The page variant is unchanged: still the full-pane screen with its back row.
    renderWithClientProp(<SettingsScreen client={null} onNavigateBack={() => {}} />);
    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-variant", "page");
    expect(screen.getByTestId("settings-back-button")).toBeInTheDocument();
  });
});
