import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  SettingsEmptyCard,
  SettingsGroup,
  SettingsList,
  SettingsListRow,
  SettingsRow,
  SettingsStatStrip,
} from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";

describe("settings layout primitives", () => {
  it("SettingsGroup renders rows with hairlines", () => {
    render(
      <SettingsGroup title="Appearance" description="How the app looks" testId="group">
        <SettingsRow label="Theme" control={<span>system</span>} />
        <SettingsRow label="Font size" control={<span>medium</span>} />
      </SettingsGroup>
    );

    expect(screen.getByText("Appearance")).toBeInTheDocument();
    expect(screen.getByText("How the app looks")).toBeInTheDocument();
    expect(screen.getByText("Theme")).toBeInTheDocument();
    expect(screen.getByText("Font size")).toBeInTheDocument();

    const group = screen.getByTestId("group");
    expect(group.querySelector("[data-slot=settings-row] + [data-slot=settings-row]")).not.toBeNull();
    expect(group.querySelector(".rounded-xl")).not.toBeNull();
  });

  it("SettingsGroup renders an actions slot beside the title", () => {
    render(
      <SettingsGroup title="T" actions={<button>Go</button>}>
        <SettingsRow label="R" />
      </SettingsGroup>
    );

    expect(screen.getByText("T")).toBeVisible();
    expect(screen.getByRole("button", { name: "Go" })).toBeVisible();
  });

  it("SettingsRow shows label, description and control", () => {
    render(
      <SettingsRow
        label="Public"
        description="Anyone with the link can view"
        control={<Switch aria-label="Public" />}
      />
    );

    expect(screen.getByText("Public")).toBeInTheDocument();
    expect(screen.getByText("Anyone with the link can view")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Public" })).toBeInTheDocument();
  });

  it("SettingsEmptyCard shows actions", () => {
    render(
      <SettingsEmptyCard
        title="No providers yet"
        description="Add a provider to get started"
        actions={<Button>Add provider</Button>}
      />
    );

    expect(screen.getByText("No providers yet")).toBeInTheDocument();
    expect(screen.getByText("Add a provider to get started")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add provider" })).toBeInTheDocument();

    const card = screen.getByText("No providers yet").closest("div");
    expect(card?.className).toContain("border-dashed");
  });

  it("SettingsStatStrip renders every tile", () => {
    render(
      <SettingsStatStrip
        stats={[
          { label: "Runs", value: 12 },
          { label: "Tasks", value: 3 },
          { label: "Peers", value: 1 },
        ]}
      />
    );

    for (const label of ["Runs", "Tasks", "Peers"]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    for (const value of ["12", "3", "1"]) {
      expect(screen.getByText(value)).toBeInTheDocument();
    }
  });

  it("ui_cleanup_b2_settings_list_is_one_divided_card", () => {
    render(
      <SettingsList testId="list">
        <SettingsListRow>One</SettingsListRow>
        <SettingsListRow>Two</SettingsListRow>
      </SettingsList>
    );

    const list = screen.getByTestId("list");
    expect(list).toHaveAttribute("data-slot", "settings-list");
    expect(list.tagName).toBe("UL");
    expect(list).toHaveClass("rounded-xl", "border-border", "bg-card", "overflow-hidden");
    // Rows are divided by a hairline, not by a gap between separate cards.
    expect(list.className).toContain("[&>*+*]:border-t");
    expect(list.className).toContain("[&>*+*]:border-border");
    expect(list.querySelectorAll("[data-slot='settings-list-row']")).toHaveLength(2);
  });

  it("ui_cleanup_b2_settings_list_row_padding", () => {
    render(
      <SettingsList>
        <SettingsListRow testId="row" className="flex items-center justify-between">
          Label
        </SettingsListRow>
      </SettingsList>
    );

    const row = screen.getByTestId("row");
    expect(row).toHaveAttribute("data-slot", "settings-list-row");
    expect(row.tagName).toBe("LI");
    expect(row).toHaveClass("px-4", "py-3");
    // A caller's layout classes merge with the padding, they don't replace it.
    expect(row).toHaveClass("flex", "items-center", "justify-between");
  });
});
