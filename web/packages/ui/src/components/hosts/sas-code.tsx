import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";

/**
 * The pairing SAS as a copyable code block: the formatted code beside a copy button that flips to a
 * check for 1.5 s after a successful copy. Clipboard access can be denied (invite-host-dialog ignores
 * that too); the code stays selectable, so the failure is swallowed silently.
 */
export function SasCode({ code, digits, testId, copyTestId }: { code: string; digits: string; testId: string; copyTestId: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );

  async function copy() {
    try {
      await navigator.clipboard.writeText(digits);
    } catch {
      return;
    }
    if (timer.current !== null) clearTimeout(timer.current);
    setCopied(true);
    timer.current = setTimeout(() => setCopied(false), 1500);
  }

  return (
    <span className="inline-flex items-center gap-2 rounded-lg border border-border bg-muted px-3 py-1.5">
      <span data-testid={testId} className="font-mono text-ui-lg font-semibold tracking-widest tabular-nums text-foreground">
        {code}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        data-testid={copyTestId}
        aria-label={copied ? "Copied" : "Copy code"}
        onClick={() => void copy()}
      >
        {copied ? <Icons.check aria-hidden /> : <Icons.copy aria-hidden />}
      </Button>
    </span>
  );
}
