import { ProviderGlyph } from "@/components/composer/provider-glyph";
import { ICON_SIZE, Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * The provider's mark by `provider` id, or a generic gauge for anything
 * else (AC39: unknown providers never throw). Marks inherit `currentColor`.
 */
export function UsageSourceIcon({ provider, className }: { provider: string; className?: string }) {
  const cls = cn(ICON_SIZE.md, "shrink-0 text-muted-foreground", className);
  switch (provider) {
    case "anthropic":
      return <ProviderGlyph provider="claude" testId={null} className={cls} />;
    case "openai":
      return <ProviderGlyph provider="codex" testId={null} className={cls} />;
    default:
      return <Icons.usage aria-hidden className={cls} />;
  }
}
