/** The owning host's alias as a small chip (fleet F4); nothing for an empty alias. */
export function HostChip({ alias, testId = "host-chip" }: { alias: string; testId?: string }) {
  if (!alias) return null;
  return (
    <span
      data-testid={testId}
      title={`Owned by ${alias}`}
      className="rounded-md border bg-surface px-1.5 py-0.5 text-ui-xs text-foreground-muted"
    >
      {alias}
    </span>
  );
}
