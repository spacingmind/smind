import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SendToAgentMenu, type SendEntry } from "@/components/send-to-agent-menu";

afterEach(cleanup);

/** Radix's trigger opens on pointerdown, not click -- same as tab-registry.test.tsx's + menu. */
function open() {
  fireEvent.pointerDown(screen.getByTestId("send-to-agent"), { button: 0 });
}

function close() {
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
}

describe("SendToAgentMenu (DT3-C)", () => {
  it("the trigger shows its label at every width, with a matching tooltip and aria-label", () => {
    render(<SendToAgentMenu getEntries={() => []} />);
    const trigger = screen.getByTestId("send-to-agent");
    expect(trigger.textContent).toContain("Send to agent");
    expect(trigger).toHaveAttribute("aria-label", "Send to agent");
    expect(trigger).toHaveAttribute("title", "Send to agent");
    const label = trigger.querySelector("span");
    expect(label?.className).not.toContain("sr-only");
    expect(label).toHaveTextContent("Send to agent");
  });

  it("shows each entry's size once open, and opening alone calls no onSelect", async () => {
    const selection = vi.fn();
    const last = vi.fn();
    render(
      <SendToAgentMenu
        getEntries={() => [
          { id: "selection", label: "Selection", detail: "12 B", onSelect: selection },
          { id: "last-100", label: "Last 100 lines", detail: "1.2 KiB", onSelect: last },
        ]}
      />,
    );

    open();
    const items = await screen.findAllByRole("menuitem");
    expect(items.map((i) => i.textContent)).toEqual(["Selection12 B", "Last 100 lines1.2 KiB"]);
    expect(screen.getByText("Adds to the agent's chat. Nothing is sent until you send.")).toBeInTheDocument();
    expect(selection).not.toHaveBeenCalled();
    expect(last).not.toHaveBeenCalled();
  });

  it("a disabled entry cannot be chosen", async () => {
    const onSelect = vi.fn();
    render(
      <SendToAgentMenu
        getEntries={() => [{ id: "selection", label: "Selection", detail: "nothing selected", disabled: true, onSelect }]}
      />,
    );

    open();
    fireEvent.click(await screen.findByRole("menuitem", { name: /Selection/ }));
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("choosing an entry calls its onSelect exactly once", async () => {
    const selection = vi.fn();
    const last = vi.fn();
    render(
      <SendToAgentMenu
        getEntries={() => [
          { id: "selection", label: "Selection", detail: "12 B", onSelect: selection },
          { id: "last-100", label: "Last 100 lines", detail: "1.2 KiB", onSelect: last },
        ]}
      />,
    );

    open();
    fireEvent.click(await screen.findByRole("menuitem", { name: /Last 100 lines/ }));
    expect(last).toHaveBeenCalledTimes(1);
    expect(selection).not.toHaveBeenCalled();
  });

  it("computes entries once per open, so sizes are fresh", async () => {
    const getEntries = vi.fn(() => [] as SendEntry[]);
    render(<SendToAgentMenu getEntries={getEntries} />);

    open();
    await screen.findByRole("menu");
    close();

    open();
    await screen.findByRole("menu");
    expect(getEntries).toHaveBeenCalledTimes(2);
  });
});
