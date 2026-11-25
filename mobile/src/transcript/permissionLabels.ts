// permissionLabels.ts: the pure long-label rule for permission options. A
// label longer than LONG_OPTION_LABEL_CHARS (after trim) is rendered in
// PermissionCard's full-width optionWide row so the native button can wrap it.

export const LONG_OPTION_LABEL_CHARS = 40;

export function isLongOptionLabel(label: string): boolean {
  return label.trim().length > LONG_OPTION_LABEL_CHARS;
}
