import { Icons } from "@/lib/icons";
import { isRateLimitedLine, parseChildEventPrompt } from "@/lib/child-event";

/**
 * A run whose prompt is the daemon's child-event delivery (ADR-0041): not
 * something the user typed, so it renders as a muted system row, not a
 * bubble. Each child's status line is plain text; the child's own last
 * message is untrusted data from another agent, so it sits behind a
 * collapsed "Show response" and is only ever a React text node.
 */
export function ChildUpdateRow({ prompt }: { prompt: string }) {
  const { blocks } = parseChildEventPrompt(prompt);
  return (
    <div data-testid="child-update" data-chat-find-text="true" className="flex flex-col gap-1.5 text-ui-sm text-foreground-muted">
      <p className="flex items-center gap-2">
        <Icons.agent aria-hidden className="size-3 shrink-0" />
        Child update
      </p>
      {blocks.length === 0 ? (
        // Unparseable (or empty): show what arrived, as text.
        prompt.trim() !== "" && (
          <p data-testid="child-update-raw" className="pl-5 break-words whitespace-pre-wrap">
            {prompt}
          </p>
        )
      ) : (
        <ul className="flex flex-col gap-2 pl-5">
          {blocks.map((block, i) => (
            <li key={i} data-testid="child-update-block" className="flex flex-col gap-0.5">
              <p
                data-testid="child-update-line"
                data-tone={isRateLimitedLine(block.line) ? "warning" : "neutral"}
                className={`break-words ${isRateLimitedLine(block.line) ? "text-warning" : ""}`}
              >
                {block.line}
              </p>
              {block.response !== undefined && (
                <details className="group/details">
                  <summary className="-mx-2 flex w-fit cursor-pointer items-center gap-1.5 rounded-md px-2 py-0.5 select-none hover:bg-hover [&::-webkit-details-marker]:hidden">
                    <Icons.chevronRight aria-hidden className="size-3 shrink-0 transition-transform group-open/details:rotate-90" />
                    Show response
                  </summary>
                  <div data-testid="child-update-response" className="py-1 pl-5 break-words whitespace-pre-wrap">
                    {block.response}
                  </div>
                </details>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
