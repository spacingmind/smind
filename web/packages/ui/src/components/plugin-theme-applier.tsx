import { useEffect, useMemo } from "react";

import { usePluginThemesQuery } from "@/hooks/use-plugin-contributions";
import { useTheme } from "@/hooks/use-theme";
import { applyPluginTheme, clearPluginThemeTokens } from "@/lib/plugin-theme";
import { usePluginThemeStore } from "@/stores/plugin-theme-store";

/**
 * Keeps the selected plugin theme on the page: applies it for the scheme in effect, follows a plugin update's new
 * tokens, and falls back to the app's own theme the moment the daemon says the theme is gone (plugin disabled,
 * removed or the global switch off). Nothing is cleared while the daemon has not answered or is unreachable, so an
 * offline reload keeps the remembered theme. Renders nothing. Its effect runs in the same flush as ThemeProvider's
 * `dark` class toggle (children first), so there is no frame with one scheme's tokens under the other's class.
 */
export function PluginThemeApplier() {
  const { resolved } = useTheme();
  const selected = usePluginThemeStore((state) => state.value);
  const setSelected = usePluginThemeStore((state) => state.set);
  const query = usePluginThemesQuery();
  const answered = query.isSuccess;

  const live = useMemo(() => {
    if (!selected) return null;
    if (!answered) return selected.theme;
    return query.data.find((t) => t.qualified === selected.id) ?? null;
  }, [selected, answered, query.data]);

  useEffect(() => {
    if (!selected) return;
    if (answered && live === null) {
      setSelected(null);
    } else if (live && JSON.stringify(live) !== JSON.stringify(selected.theme)) {
      setSelected({ id: selected.id, theme: live });
    }
  }, [selected, answered, live, setSelected]);

  useEffect(() => {
    applyPluginTheme(resolved, live);
  }, [resolved, live]);

  useEffect(() => clearPluginThemeTokens, []);
  return null;
}
