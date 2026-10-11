import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { McpServersIndicator } from "@/components/mcp-servers-indicator";
import type { McpServerState } from "@/lib/types";

function server(overrides: Partial<McpServerState> = {}): McpServerState {
  return { name: "github", transport: "stdio", status: "active", ...overrides };
}

describe("McpServersIndicator", () => {
  it("renders nothing without servers", () => {
    const { container } = render(<McpServersIndicator servers={undefined} />);
    expect(container).toBeEmptyDOMElement();

    const { container: empty } = render(<McpServersIndicator servers={[]} />);
    expect(empty).toBeEmptyDOMElement();
  });

  it("shows MCP 2 when all servers are active", () => {
    render(<McpServersIndicator servers={[server(), server({ name: "search", transport: "http" })]} />);

    const pill = screen.getByTestId("run-mcp-indicator");
    expect(pill).toHaveTextContent("MCP 2");
    expect(pill).not.toHaveTextContent("MCP 2/2");
    expect(pill).toHaveAccessibleName("MCP servers: 2 of 2 active");
  });

  it("shows MCP 1/3 with a warning marker when servers are dropped or failed", () => {
    render(
      <McpServersIndicator
        servers={[
          server(),
          server({ name: "search", transport: "http", status: "dropped", reason: "transport_unsupported" }),
          server({ name: "db", status: "failed", reason: "failed_to_start" }),
        ]}
      />,
    );

    const pill = screen.getByTestId("run-mcp-indicator");
    expect(pill).toHaveTextContent("MCP 1/3");
    expect(pill.querySelector(".bg-warning")).not.toBeNull();
    expect(pill).toHaveAccessibleName("MCP servers: 1 of 3 active");
  });

  it("popover lists each server with transport and reason text", async () => {
    render(
      <McpServersIndicator
        servers={[
          server(),
          server({ name: "search", transport: "http", status: "dropped", reason: "transport_unsupported" }),
          server({ name: "db", status: "failed", reason: "command_not_found" }),
          server({ name: "extra", transport: "sse", status: "dropped", reason: "provider_unsupported" }),
          server({ name: "odd", status: "dropped" }),
        ]}
      />,
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("run-mcp-indicator"));
    });

    const popover = screen.getByTestId("run-mcp-popover");
    expect(popover).toHaveTextContent("MCP servers");
    expect(popover).toHaveTextContent("github");
    expect(popover).toHaveTextContent("stdio");
    expect(popover).toHaveTextContent("Active");
    expect(popover).toHaveTextContent("search");
    expect(popover).toHaveTextContent("http");
    expect(popover).toHaveTextContent("Not active — agent doesn't support this transport");
    expect(popover).toHaveTextContent("Not active — command not found in PATH");
    expect(popover).toHaveTextContent("Not active — not supported by this provider yet");
    // No/unknown reason degrades to the bare label, not a crash.
    expect(popover).toHaveTextContent("Not active — not active");
  });

  it("never renders a secret-looking value", () => {
    // The component only ever receives McpServerState; this smuggles
    // extra fields past the type to prove they can't leak into the DOM.
    const smuggled = [
      {
        name: "github",
        transport: "stdio" as const,
        status: "failed" as const,
        reason: "command_not_found" as const,
        env: { TOKEN: "SECRET-XYZ" },
        command: "npx -y secret-server",
        url: "https://secret.example.internal/mcp",
      },
    ];

    render(<McpServersIndicator servers={smuggled} />);
    expect(document.body.textContent).not.toContain("SECRET-XYZ");
    expect(document.body.textContent).not.toContain("npx -y secret-server");
    expect(document.body.textContent).not.toContain("secret.example.internal");
  });
});
