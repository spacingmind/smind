import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AutoAcceptToggle } from "@/components/pickers/auto-accept-toggle";
import { ModeSelect } from "@/components/pickers/mode-select";
import { ThinkingSelect } from "@/components/pickers/thinking-select";
import { THINKING_LEVELS } from "@/lib/thinking-levels";

const MODES = [
  { id: "acceptEdits", label: "Accept File Edits", description: "Automatically approves edit-focused tools without prompting" },
  { id: "default", label: "Always Ask", description: "Prompts for permission the first time a tool is used" },
  { id: "bypassPermissions", label: "Bypass", description: "Skip all permission prompts (use with caution)", autoApproves: true },
];

async function pickOption(label: string): Promise<void> {
  // Radix Select: open via the trigger, then click the option.
  await Promise.resolve();
  fireEvent.click(await screen.findByRole("option", { name: label }));
}

describe("ModeSelect", () => {
  it.each(["pill", "field"] as const)("renders and fires onChange with the mode id (%s variant)", async (variant) => {
    const onChange = vi.fn();
    render(<ModeSelect modes={MODES} value="acceptEdits" onChange={onChange} variant={variant} testId="mode-select" />);

    const trigger = screen.getByLabelText("Permission mode");
    expect(trigger).toBeInTheDocument();
    expect(screen.getByTestId("mode-select")).toBe(trigger);
    expect(trigger).toHaveTextContent("Accept File Edits");
    expect(within(document.body).queryAllByRole("option")).toHaveLength(0);

    fireEvent.click(trigger);
    await pickOption("Bypass");
    expect(onChange).toHaveBeenCalledWith("bypassPermissions");
  });

  it.each(["pill", "field"] as const)("disabled blocks opening and changing (%s variant)", (variant) => {
    const onChange = vi.fn();
    render(<ModeSelect modes={MODES} value="acceptEdits" onChange={onChange} disabled variant={variant} />);

    const trigger = screen.getByLabelText("Permission mode");
    expect(trigger).toBeDisabled();
    fireEvent.click(trigger);
    expect(within(document.body).queryAllByRole("option")).toHaveLength(0);
    expect(onChange).not.toHaveBeenCalled();
  });

  it.each(["pill", "field"] as const)("dangerous sets data-danger and warning styling (%s variant)", (variant) => {
    render(<ModeSelect modes={MODES} value="bypassPermissions" onChange={() => {}} dangerous variant={variant} />);
    const trigger = screen.getByLabelText("Permission mode");
    expect(trigger).toHaveAttribute("data-danger", "true");
    // Both variants warn in the warning colour (the pill also swaps its icon).
    expect(trigger).toHaveClass("text-warning");
  });

  it("field variant can lead with a Provider default item", async () => {
    const onChange = vi.fn();
    render(
      <ModeSelect
        modes={MODES}
        value=""
        onChange={onChange}
        variant="field"
        extraItems={[{ id: "", label: "Provider default" }]}
      />,
    );

    // An empty value names the "Provider default" entry itself (Radix would show the bare placeholder).
    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Provider default");
    fireEvent.click(screen.getByLabelText("Permission mode"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Provider default", "Accept File Edits", "Always Ask", "Bypass"]);
    fireEvent.click(options[3]);
    expect(onChange).toHaveBeenCalledWith("bypassPermissions");

  });

  it("the leading Provider default item is selectable and fires the empty id (Radix accepts it)", async () => {
    const onChange = vi.fn();
    render(
      <ModeSelect modes={MODES} value="acceptEdits" onChange={onChange} variant="field" extraItems={[{ id: "", label: "Provider default" }]} />,
    );
    fireEvent.click(screen.getByLabelText("Permission mode"));
    fireEvent.click((await screen.findAllByRole("option"))[0]);
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("mode items show description and check", async () => {
    render(
      <ModeSelect
        modes={[{ id: "default", label: "Always Ask", description: "Asks before running tools" }, { id: "plan", label: "Plan" }]}
        value="default"
        onChange={() => {}}
        variant="pill"
      />,
    );
    const trigger = screen.getByLabelText("Permission mode");
    fireEvent.click(trigger);
    expect(await screen.findByText("Asks before running tools")).toBeVisible();
    expect(screen.getByRole("option", { name: "Always Ask" })).toHaveAttribute("data-state", "checked");
    expect(screen.getByRole("option", { name: "Plan" })).toHaveAttribute("data-state", "unchecked");
    expect(trigger).toHaveTextContent("Always Ask");
    expect(trigger).not.toHaveTextContent("Asks before running tools");
  });

  it("items carry their description as title", async () => {
    render(<ModeSelect modes={MODES} value="acceptEdits" onChange={() => {}} variant="pill" />);
    expect(screen.getByLabelText("Permission mode")).toHaveAttribute("title", MODES[0].description);
  });
});

describe("ThinkingSelect", () => {
  it.each(["pill", "field"] as const)("renders and fires onChange with the level id (%s variant)", async (variant) => {
    const onChange = vi.fn();
    render(<ThinkingSelect value="standard" onChange={onChange} variant={variant} testId="thinking-select" />);

    const trigger = screen.getByLabelText("Thinking level");
    expect(screen.getByTestId("thinking-select")).toBe(trigger);
    expect(trigger).toHaveTextContent("Standard");

    fireEvent.click(trigger);
    await pickOption("Extended");
    expect(onChange).toHaveBeenCalledWith("extended");
  });

  it.each(["pill", "field"] as const)("disabled blocks changing (%s variant)", (variant) => {
    const onChange = vi.fn();
    render(<ThinkingSelect value="standard" onChange={onChange} disabled variant={variant} />);

    const trigger = screen.getByLabelText("Thinking level");
    expect(trigger).toBeDisabled();
    fireEvent.click(trigger);
    expect(within(document.body).queryAllByRole("option")).toHaveLength(0);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("title is the current level's help, and custom levels are honoured", () => {
    const levels = [
      { id: "", label: "Composer default" },
      { id: "off", label: "Off" },
      { id: "extended", label: "Extended", help: THINKING_LEVELS[2].help },
    ];
    render(<ThinkingSelect levels={levels} value="extended" onChange={() => {}} variant="field" />);
    expect(screen.getByLabelText("Thinking level")).toHaveAttribute("title", THINKING_LEVELS[2].help);
  });
});

describe("AutoAcceptToggle", () => {
  it("pill variant renders the composer toggle and fires onChange(true)", () => {
    const onChange = vi.fn();
    render(<AutoAcceptToggle checked={false} onChange={onChange} variant="pill" />);

    const button = screen.getByTestId("composer-auto-accept");
    expect(button).toHaveAttribute("aria-label", "Auto-accept");
    expect(button).toHaveAttribute("aria-pressed", "false");
    expect(button).toHaveAttribute("title", "Automatically approves every permission prompt from this agent.");
    fireEvent.click(button);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it("pill variant warns when on and marks pending", () => {
    render(<AutoAcceptToggle checked onChange={() => {}} pending variant="pill" />);
    const button = screen.getByTestId("composer-auto-accept");
    expect(button).toHaveAttribute("aria-pressed", "true");
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button).toHaveClass("text-warning");
  });

  it("pill variant disabled blocks the change", () => {
    const onChange = vi.fn();
    render(<AutoAcceptToggle checked={false} onChange={onChange} disabled variant="pill" />);
    const button = screen.getByTestId("composer-auto-accept");
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("field variant renders the checkbox label and fires onChange(false)", () => {
    const onChange = vi.fn();
    render(<AutoAcceptToggle checked onChange={onChange} variant="field" testId="form-auto-accept" />);

    const box = screen.getByTestId("form-auto-accept");
    expect(box).toBeChecked();
    expect(screen.getByText("Auto-accept every permission prompt")).toBeInTheDocument();
    fireEvent.click(box);
    expect(onChange).toHaveBeenCalledWith(false);
  });

  it("field variant disabled blocks the change", () => {
    const onChange = vi.fn();
    render(<AutoAcceptToggle checked onChange={onChange} disabled variant="field" />);
    const box = screen.getByRole("checkbox") as HTMLInputElement;
    expect(box).toBeDisabled();
  });
});
