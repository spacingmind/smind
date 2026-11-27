import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

/**
 * The one workspace `Select` shared by the Settings sections that pick a workspace
 * (Skills (learned), Browser): the same trigger, options and aria-label, so the two
 * tabs' pickers cannot drift apart.
 */
export function SettingsWorkspacePicker({
  workspaces,
  value,
  onChange,
  label = "Workspace",
  testId,
}: {
  workspaces: { id: number; title: string }[];
  value: number;
  onChange: (id: number) => void;
  label?: string;
  testId?: string;
}) {
  return (
    <Select value={String(value)} onValueChange={(v) => onChange(Number(v))}>
      <SelectTrigger aria-label={label} data-testid={testId} className="w-full max-w-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {workspaces.map((w) => (
          <SelectItem key={w.id} value={String(w.id)}>
            {w.title}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
