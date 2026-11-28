import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { InviteHostDialog } from "@/components/hosts/invite-host-dialog";
import { FleetDaemon } from "@/test/fleet-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

describe("InviteHostDialog", () => {
  // jsdom has no layout: assert the classes that make the field shrink and signal clipping.
  it("TestInviteDialog_LongInviteShrinksAndSignalsClipping", async () => {
    const d = new FleetDaemon();
    renderWithDaemon(<InviteHostDialog open onOpenChange={() => {}} />, d);
    await flush();
    fireEvent.click(screen.getByTestId("invite-create"));
    await flush();

    const field = screen.getByTestId("invite-value");
    expect(field).toHaveAttribute("readonly");
    expect(field).toHaveValue("smind-host://v1#payload");
    expect(field).toHaveAttribute("title", "smind-host://v1#payload");
    // Astryx TextInput: sizing utilities land on the field container, text utilities on the <input>.
    expect(field).toHaveClass("text-ellipsis", "font-mono");
    expect(field.closest('[class*="flex-1"]')).toHaveClass("min-w-0", "flex-1");
    expect(screen.getByTestId("invite-copy")).toHaveClass("shrink-0");
  });
});
