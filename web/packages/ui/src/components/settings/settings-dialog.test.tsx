// UI cleanup B3 (peer A): the Settings dialog -- the modal variant of the
// settings screen wrapped in the Dialog primitive. Mounts the dialog from a
// trigger button harness (the app's callers own the open state the same way)
// so focus return and real unmount behaviour are exercised, not just the
// open markup.
import { act, useState } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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

  it("ui_cleanup_b3_requested_section_is_selected_on_open", async () => {
    const requested = renderWithClientProp(<Harness initialSectionId="shortcuts" />);
    fireEvent.click(screen.getByRole("button", { name: "Open settings" }));
    await settle();
    expect(screen.getByTestId("settings-nav-shortcuts")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("settings-dialog")).toContainElement(screen.getByTestId("settings-section-shortcuts"));
    requested.unmount();

    renderWithClientProp(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Open settings" }));
    await settle();
    expect(screen.getByTestId("settings-nav-general")).toHaveAttribute("aria-current", "true");
  });

  it("ui_cleanup_b3_no_focus_ring_on_open", async () => {
    await openDialog();

    const dialog = screen.getByTestId("settings-dialog");
    expect(document.activeElement).toBe(dialog);
    expect(dialog.className).toContain("outline-none");
    expect(document.activeElement?.getAttribute("data-testid") ?? "").not.toMatch(/^settings-nav-/);
  });

  it("ui_cleanup_b3_tab_moves_focus_into_the_nav", async () => {
    await openDialog();

    // user-event is not a dependency here and jsdom does not move focus on
    // Tab, so mirror it: the first element Tab reaches (Radix's own tabbable
    // rules) is a nav button, which keeps the browser's focus-visible ring.
    const dialog = screen.getByTestId("settings-dialog");
    fireEvent.keyDown(dialog, { key: "Tab" });
    const next = tabbablesIn(dialog)[0]!;
    next.focus();
    expect(document.activeElement).toBe(next);
    expect(next.getAttribute("data-testid") ?? "").toMatch(/^settings-nav-/);
    expect(dialog).toContainElement(next);
    expect(next.className).not.toContain("outline-none");
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

describe("SettingsDialog small sizes", () => {
  it("ui_cleanup_b3_sheet_classes_at_small_sizes", async () => {
    await openDialog();

    // jsdom has no layout, so this pins the class contract: dialog sizes at
    // desktop, full-bleed sheet below 860px wide and below 560px tall.
    const content = screen.getByTestId("settings-dialog");
    expect(content.className).toContain("w-[min(960px,90vw)]");
    expect(content.className).toContain("h-[min(680px,90vh)]");
    expect(content.className).toContain("rounded-2xl");
    expect(content.className).toContain("max-[860px]:inset-0");
    expect(content.className).toContain("max-[860px]:rounded-none");
    expect(content.className).toContain("max-[860px]:border-0");
    expect(content.className).toContain("[@media(max-height:560px)]:inset-0");
    expect(content.className).toContain("[@media(max-height:560px)]:rounded-none");
    expect(content.className).toContain("[@media(max-height:560px)]:border-0");
    // A web test has no traffic lights, so the sheet tops out at the window
    // edge; only the desktop macOS build offsets below the title bar.
    expect(content).toHaveAttribute("data-lights", "false");
    expect(content.className).toContain("max-[860px]:top-0");
    expect(content.className).not.toContain("top-[38px]");
  });
});

describe("SettingsDialog narrow", () => {
  // Same shape as settings-screen.test.tsx: useIsMobile reads
  // window.innerWidth inside an effect, so each render is followed by a
  // flush to let it settle.
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 390 });
    window.matchMedia = vi.fn().mockImplementation(
      (query: string) =>
        ({
          matches: window.innerWidth < 768,
          media: query,
          onchange: null,
          addEventListener: () => {},
          removeEventListener: () => {},
          addListener: () => {},
          removeListener: () => {},
          dispatchEvent: () => false,
        }) as unknown as MediaQueryList,
    );
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 1024 });
  });

  it("ui_cleanup_b3_narrow_keeps_list_to_detail", async () => {
    await openDialog();

    // The sheet keeps today's drill-down: the list page first, no desktop sidebar.
    expect(screen.getByTestId("settings-nav-appearance")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-sidebar")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-nav-appearance"));
    await settle();

    expect(screen.queryByTestId("settings-nav-appearance")).not.toBeInTheDocument();
    expect(screen.getByTestId("settings-detail-back")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-appearance")).toBeInTheDocument();
    expect(screen.getByTestId("settings-screen-header")).toHaveTextContent("Appearance");
  });

  it("ui_cleanup_b3_narrow_esc_pops_to_the_list", async () => {
    const onClose = vi.fn();
    await openDialog(onClose);

    fireEvent.click(screen.getByTestId("settings-nav-general"));
    await settle();
    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();

    // Escape in a detail page pops to the list, not out of the dialog.
    fireEvent.keyDown(document, { key: "Escape" });
    await settle();
    expect(screen.getByTestId("settings-nav-general")).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();

    // A second Escape closes the dialog.
    fireEvent.keyDown(document, { key: "Escape" });
    await settle();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
