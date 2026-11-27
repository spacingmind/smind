import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SettingsWorkspacePicker } from "@/components/settings/settings-workspace-picker";

describe("settings-workspace-picker", () => {
  it("settings_workspace_picker_renders_options_and_reports_change", async () => {
    const onChange = vi.fn();
    render(
      <SettingsWorkspacePicker
        workspaces={[
          { id: 1, title: "alpha" },
          { id: 2, title: "beta" },
        ]}
        value={1}
        onChange={onChange}
        label="Workspace"
        testId="picker"
      />,
    );

    // The selected workspace shows in the trigger, which is named by the label.
    const trigger = screen.getByTestId("picker");
    expect(trigger).toHaveAttribute("aria-label", "Workspace");
    expect(trigger.className).toContain("w-full");
    expect(trigger.className).toContain("max-w-xs");
    expect(screen.getByText("alpha")).toBeInTheDocument();

    // Opening it lists every workspace; choosing one reports that workspace's id.
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("option", { name: "beta" }));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(2);
  });
});
