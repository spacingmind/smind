import { createContext, useContext, type ReactNode } from "react";

/**
 * True when the host (the full-screen settings screen) already shows the
 * active section's name in its header, so a section's own in-body title
 * would just repeat it. Defaults to false: a section rendered on its own
 * (its tests, any other host) keeps its title exactly as before.
 */
const SectionTitleInHeaderContext = createContext(false);

export const SectionTitleInHeaderProvider = SectionTitleInHeaderContext.Provider;

/**
 * A section's title row: the title on the left, `actions` (a primary button,
 * a refresh) on the right. Under {@link SectionTitleInHeaderProvider} the
 * title (and its optional `description`'s heading line) is dropped and only
 * the actions remain, right-aligned; with neither it renders nothing.
 */
export function SettingsSectionTitle({
  title,
  description,
  actions,
}: {
  title: string;
  /** A muted line under the title; kept when the title itself is hidden so its information isn't lost. */
  description?: ReactNode;
  actions?: ReactNode;
}) {
  const titleInHeader = useContext(SectionTitleInHeaderContext);
  if (titleInHeader && !actions && !description) return null;
  return (
    <div className="flex items-center justify-between gap-2">
      {titleInHeader ? (
        description ? <p className="text-ui-sm text-muted-foreground">{description}</p> : <span />
      ) : (
        <div>
          <h3 className="text-ui-base font-medium text-foreground">{title}</h3>
          {description && <p className="text-ui-sm text-muted-foreground">{description}</p>}
        </div>
      )}
      {actions}
    </div>
  );
}
