import { useEffect, useRef, useState, type JSX } from "react";

import { Icons } from "@/lib/icons";
import { Button } from "@/components/ui/button";

const COPY_LABEL_MS = 1500;

export function ProviderDiagnostics(props: { text: string }): JSX.Element {
  const { text } = props;
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  const onCopy = () => {
    navigator.clipboard
      .writeText(text)
      .then(() => {
        setCopied(true);
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => setCopied(false), COPY_LABEL_MS);
      })
      .catch(() => {});
  };

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        aria-expanded={open}
        data-testid="provider-diagnostics-toggle"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 text-ui-base text-muted-foreground hover:text-foreground"
      >
        {open ? (
          <Icons.chevronDown className="size-3" />
        ) : (
          <Icons.chevronRight className="size-3" />
        )}
        Diagnostics
      </button>
      <span className="text-ui-xs text-muted-foreground">Copyable text for bug reports</span>
      {open ? (
        <div className="flex flex-col gap-2">
          <pre
            data-testid="provider-diagnostics-text"
            className="max-h-64 overflow-auto rounded-md border bg-muted/50 p-3 font-mono text-ui-xs"
          >
            {text}
          </pre>
          <Button
            variant="outline"
            size="xs"
            data-testid="provider-diagnostics-copy"
            onClick={onCopy}
            className="self-start"
          >
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
