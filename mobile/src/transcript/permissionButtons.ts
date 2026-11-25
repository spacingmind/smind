// permissionButtons.ts maps a permission option's `kind` (ACP: allow_once,
// allow_always, reject_once, ...) to an @expo/ui Button variant: the FIRST
// allow* option is the filled primary, other allow* options are outlined,
// reject* options are quiet text buttons, unknown kinds are outlined.
// Exception: a browser security card (detail kind "browser") has NO filled
// primary -- every allow* option is outlined, so no option is recommended.

export type PermissionButtonVariant = 'filled' | 'outlined' | 'text';

export function permissionButtonVariants(options: ReadonlyArray<{ kind: string }>, detailKind?: string): PermissionButtonVariant[] {
  let primaryTaken = false;
  return options.map((opt) => {
    if (opt.kind.startsWith('allow')) {
      if (primaryTaken || detailKind === 'browser') return 'outlined';
      primaryTaken = true;
      return 'filled';
    }
    if (opt.kind.startsWith('reject')) return 'text';
    return 'outlined';
  });
}
