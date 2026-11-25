import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { BrowserAllowlistEditor } from "@/components/settings/browser-allowlist-editor";

/** A promise the test settles by hand, so a call can be held pending. */
function deferred(): { promise: Promise<void>; resolve: () => void; reject: (error: Error) => void } {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function hostInput() {
  return screen.getByRole("textbox", { name: "Host to allow" });
}

describe("browser allowlist editor", () => {
  it("shows the localhost-only text when empty", () => {
    render(<BrowserAllowlistEditor hosts={[]} onAdd={vi.fn()} onRemove={vi.fn()} />);
    expect(screen.getByTestId("browser-allow-empty")).toHaveTextContent("Only localhost is reachable.");
    expect(screen.queryByTestId("browser-allow-list")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-allow-count")).toHaveTextContent("0 of 50 hosts");
    expect(hostInput()).toBeInTheDocument();
  });

  it("adds by button and by Enter and clears the input", async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined);
    render(<BrowserAllowlistEditor hosts={[]} onAdd={onAdd} onRemove={vi.fn()} />);
    const input = hostInput();

    fireEvent.change(input, { target: { value: "  intranet.example  " } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(input).toHaveValue(""));
    expect(onAdd).toHaveBeenCalledWith("intranet.example");

    fireEvent.change(input, { target: { value: "192.0.2.10" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(input).toHaveValue(""));
    expect(onAdd).toHaveBeenCalledWith("192.0.2.10");
    expect(onAdd).toHaveBeenCalledTimes(2);
  });

  it("keeps the typed value and shows the daemon's text verbatim with aria-invalid when the add is refused", async () => {
    const onAdd = vi.fn().mockRejectedValue(new Error("wildcards are not allowed"));
    render(<BrowserAllowlistEditor hosts={[]} onAdd={onAdd} onRemove={vi.fn()} />);
    const input = hostInput();
    fireEvent.change(input, { target: { value: "*.example.com" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(screen.getByTestId("browser-allow-error")).toHaveTextContent("wildcards are not allowed"));
    expect(input).toHaveValue("*.example.com");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("browser-allow-count")).toHaveTextContent("0 of 50 hosts");
  });

  it("typing clears the error", async () => {
    const onAdd = vi.fn().mockRejectedValue(new Error("wildcards are not allowed"));
    render(<BrowserAllowlistEditor hosts={[]} onAdd={onAdd} onRemove={vi.fn()} />);
    const input = hostInput();
    fireEvent.change(input, { target: { value: "*.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.getByTestId("browser-allow-error")).toBeInTheDocument());

    fireEvent.change(input, { target: { value: "*.example.co" } });
    expect(screen.queryByTestId("browser-allow-error")).not.toBeInTheDocument();
    expect(input).not.toHaveAttribute("aria-invalid");
  });

  it("empty input does not call onAdd", () => {
    const onAdd = vi.fn();
    render(<BrowserAllowlistEditor hosts={[]} onAdd={onAdd} onRemove={vi.fn()} />);
    const input = hostInput();
    const add = screen.getByRole("button", { name: "Add" });

    expect(add).toBeDisabled();
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onAdd).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "   " } });
    expect(add).toBeDisabled();
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.click(add);
    expect(onAdd).not.toHaveBeenCalled();
  });

  it("the cap disables add and says the list is full", () => {
    const hosts = Array.from({ length: 50 }, (_, i) => `host-${i}.example`);
    const { rerender } = render(<BrowserAllowlistEditor hosts={hosts} onAdd={vi.fn()} onRemove={vi.fn()} />);
    expect(hostInput()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
    expect(screen.getByTestId("browser-allow-full")).toHaveTextContent("The list is full (50 hosts).");
    expect(screen.getByTestId("browser-allow-count")).toHaveTextContent("50 of 50 hosts");

    rerender(<BrowserAllowlistEditor hosts={["a.example", "b.example"]} onAdd={vi.fn()} onRemove={vi.fn()} max={2} />);
    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
    expect(hostInput()).toBeDisabled();
    expect(screen.getByTestId("browser-allow-full")).toHaveTextContent("The list is full (2 hosts).");
  });

  it("remove calls onRemove with the host and is labelled by host", async () => {
    const onRemove = vi.fn().mockResolvedValue(undefined);
    render(<BrowserAllowlistEditor hosts={["intranet.example"]} onAdd={vi.fn()} onRemove={onRemove} />);
    expect(screen.getByRole("list", { name: "Allowed hosts" })).toBeInTheDocument();

    const remove = screen.getByRole("button", { name: "Remove intranet.example" });
    expect(remove).toHaveTextContent("Remove");
    fireEvent.click(remove);
    await waitFor(() => expect(onRemove).toHaveBeenCalledWith("intranet.example"));
  });

  it("a pending add disables the input and the button", async () => {
    const pending = deferred();
    const onAdd = vi.fn(() => pending.promise);
    render(<BrowserAllowlistEditor hosts={[]} onAdd={onAdd} onRemove={vi.fn()} />);
    const input = hostInput();
    const add = screen.getByRole("button", { name: "Add" });

    fireEvent.change(input, { target: { value: "intranet.example" } });
    fireEvent.click(add);
    expect(input).toBeDisabled();
    expect(add).toBeDisabled();
    expect(input.parentElement).toHaveAttribute("aria-busy", "true");

    pending.resolve();
    await waitFor(() => expect(input).not.toBeDisabled());
    await waitFor(() => expect(input).toHaveValue(""));
    expect(onAdd).toHaveBeenCalledWith("intranet.example");
  });

  it("a refused remove shows the text", async () => {
    const onRemove = vi.fn().mockRejectedValue(new Error("this host is in use by a running session"));
    render(<BrowserAllowlistEditor hosts={["intranet.example"]} onAdd={vi.fn()} onRemove={onRemove} />);

    fireEvent.click(screen.getByRole("button", { name: "Remove intranet.example" }));
    await waitFor(() =>
      expect(screen.getByTestId("browser-allow-error")).toHaveTextContent("this host is in use by a running session")
    );
    // A refused remove is the list's error, not the input's.
    expect(hostInput()).not.toHaveAttribute("aria-invalid");
  });

  it("the note is shown", () => {
    const note =
      "a host name may only resolve to public addresses; to allow an intranet or local-network host, add its IP address instead";
    render(<BrowserAllowlistEditor hosts={[]} onAdd={vi.fn()} onRemove={vi.fn()} note={note} />);
    expect(screen.getByText(note)).toBeInTheDocument();
  });

  it("disabled disables everything", () => {
    const onAdd = vi.fn();
    const onRemove = vi.fn();
    render(
      <BrowserAllowlistEditor hosts={["intranet.example"]} onAdd={onAdd} onRemove={onRemove} disabled />
    );
    expect(hostInput()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Remove intranet.example" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Remove intranet.example" }));
    expect(onRemove).not.toHaveBeenCalled();
  });
});
