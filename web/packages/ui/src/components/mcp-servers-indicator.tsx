import { Plug } from "lucide-react";

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { McpServerState } from "@/lib/types";

const NOT_ACTIVE_REASONS: Partial<Record<NonNullable<McpServerState["reason"]>, string>> = {
  transport_unsupported: "transport not supported",
  command_not_found: "command not found in PATH",
  provider_unsupported: "not supported by this provider yet",
  failed_to_start: "failed to start",
};

/**
 * The chat header's per-run "active MCP servers" pill
 * (docs/plans/active/agent-mcp-servers.md): a count of the run's active
 * MCP servers, with a popover listing each one's name, transport, and
 * state. Renders nothing when the run reported no servers. Only ever
 * receives McpServerState -- name/transport/status/reason and nothing
 * else, so a server's command/env/url can never leak into the DOM.
 */
export function McpServersIndicator({ servers }: { servers: McpServerState[] | undefined }) {
  if (!servers || servers.length === 0) return null;

  const active = servers.filter((s) => s.status === "active").length;
  const allActive = active === servers.length;

  return (
    <Popover>
      <PopoverTrigger
        type="button"
        data-testid="run-mcp-indicator"
        aria-label={`MCP servers: ${active} of ${servers.length} active`}
        className="flex shrink-0 items-center gap-1 truncate rounded-full border px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground"
      >
        <Plug className="size-3" />
        <span className="truncate">
          {allActive ? `MCP ${active}` : `MCP ${active}/${servers.length}`}
        </span>
        {!allActive && <span className="size-1.5 shrink-0 rounded-full bg-warning" />}
      </PopoverTrigger>
      <PopoverContent data-testid="run-mcp-popover" align="start" className="w-72 p-3">
        <p className="text-ui-sm font-medium text-foreground">MCP servers</p>
        <ul className="mt-2 space-y-2">
          {servers.map((server) => (
            <li key={server.name} className="flex flex-col gap-0.5">
              <span className="flex items-center gap-1.5">
                <span className="truncate text-ui-sm font-medium text-foreground">{server.name}</span>
                <span className="shrink-0 rounded border px-1 text-ui-sm text-foreground-muted">{server.transport}</span>
              </span>
              <span className={`text-ui-sm ${server.status === "active" ? "text-foreground-muted" : "text-warning"}`}>
                {server.status === "active"
                  ? "Active"
                  : `Not active — ${NOT_ACTIVE_REASONS[server.reason!] ?? "not active"}`}
              </span>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}
