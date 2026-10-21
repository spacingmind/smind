import { useState } from "react";

import { DiffRender } from "@/components/diff-render";
import { Button } from "@/components/ui/button";
import { formatBytes } from "@/lib/probe-format";
import { Icons } from "@/lib/icons";

/** What the skill arm changed in its sandbox checkout, collapsed until asked for (a pure UI toggle). */
export function ProbeDiff({ diff, diffBytes }: { diff: string; diffBytes: number }) {
  const [open, setOpen] = useState(false);
  const Chevron = open ? Icons.chevronDown : Icons.chevronRight;
  return (
    <div className="flex flex-col gap-2" data-testid="probe-diff">
      <Button
        type="button"
        variant="quiet"
        size="sm"
        className="self-start"
        aria-expanded={open}
        data-testid="probe-diff-toggle"
        onClick={() => setOpen((v) => !v)}
      >
        <Chevron aria-hidden /> Changes made with this skill ({formatBytes(diffBytes)})
      </Button>
      {open && (
        <div className="max-h-96 overflow-auto rounded-lg border border-border" tabIndex={0} role="region" aria-label="Changes made with this skill">
          <DiffRender diff={diff} outputFormat="line-by-line" testId="probe-diff-body" />
        </div>
      )}
    </div>
  );
}
