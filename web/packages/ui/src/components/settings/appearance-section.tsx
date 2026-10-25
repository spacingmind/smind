
import { registerSettingsSection } from "@/components/settings/settings-registry";
import { StepGroup } from "@/components/ui/step-group";
import { useFontSizes } from "@/hooks/use-font-sizes";
import { usePluginThemes } from "@/hooks/use-plugin-contributions";
import { useTheme } from "@/hooks/use-theme";
import { usePluginThemeStore } from "@/stores/plugin-theme-store";
import type { FontSizes, FontSizeStep } from "@/lib/settings-preferences";
import type { ThemePreference } from "@/lib/theme";
import { Icons } from "@/lib/icons";

const THEME_OPTIONS: { value: ThemePreference; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

const FONT_SIZE_OPTIONS: { value: FontSizeStep; label: string }[] = [
  { value: "small", label: "Small" },
  { value: "medium", label: "Medium" },
  { value: "large", label: "Large" },
];

const FONT_SIZE_AXES: { axis: keyof FontSizes; label: string; description: string }[] = [
  { axis: "interface", label: "Interface", description: "Sidebar, menus and other UI chrome" },
  { axis: "content", label: "Content", description: "Chat and task prose" },
  { axis: "code", label: "Code", description: "Diffs, file contents and the terminal" },
];

/**
 * Item 13's Appearance section: theme plus the interface/content/code
 * font-size split (audit-paseo.md §6 -- "the one that actually matters
 * for an app that mixes chat prose, UI chrome and code"). Registers
 * itself at import time (see settings-registry.ts's doc comment); the
 * settings screen never references this file directly.
 */
export function AppearanceSection() {
  const { preference, resolved, setPreference } = useTheme();
  const pluginThemes = usePluginThemes();
  const selectedTheme = usePluginThemeStore((state) => state.value);
  const setSelectedTheme = usePluginThemeStore((state) => state.set);
  const themeOptions = [
    { value: "", label: "Default" },
    ...pluginThemes.map((t) => ({ value: t.qualified, label: `${t.label} · ${t.plugin}` })),
  ];
  const chosen = pluginThemes.find((t) => t.qualified === selectedTheme?.id);
  const lacksScheme = chosen !== undefined && chosen.schemes[resolved] === undefined;
  const { fontSizes, setFontSize } = useFontSizes();

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-appearance">
      <section className="flex flex-col gap-2">
        <h3 className="text-ui-base font-medium text-foreground">Theme</h3>
        <StepGroup groupLabel="theme" options={THEME_OPTIONS} value={preference} onChange={setPreference} />
      </section>

      {pluginThemes.length > 0 && (
        <section className="flex flex-col gap-2" data-testid="settings-plugin-themes">
          <h3 className="text-ui-base font-medium text-foreground">Plugin theme</h3>
          <StepGroup
            wrap
            groupLabel="plugin-theme"
            options={themeOptions}
            value={chosen?.qualified ?? ""}
            onChange={(id) => {
              const theme = pluginThemes.find((t) => t.qualified === id);
              setSelectedTheme(theme ? { id: theme.qualified, theme } : null);
            }}
          />
          {lacksScheme && (
            <p className="text-ui-sm text-muted-foreground" data-testid="settings-plugin-theme-note">
              {chosen.label} has no {resolved} variant, so the default {resolved} theme stays.
            </p>
          )}
        </section>
      )}

      <section className="flex flex-col gap-4">
        <h3 className="text-ui-base font-medium text-foreground">Font size</h3>
        {FONT_SIZE_AXES.map(({ axis, label, description }) => (
          <div key={axis} className="flex items-center justify-between gap-4">
            <div>
              <p className="text-ui-base text-foreground">{label}</p>
              <p className="text-ui-sm text-muted-foreground">{description}</p>
            </div>
            <StepGroup
              groupLabel={`font-size-${axis}`}
              options={FONT_SIZE_OPTIONS}
              value={fontSizes[axis]}
              onChange={(step) => setFontSize(axis, step)}
            />
          </div>
        ))}
      </section>
    </div>
  );
}

registerSettingsSection({
  id: "appearance",
  label: "Appearance",
  order: 100,
  icon: <Icons.appearance aria-hidden className="size-3.5" />,
  render: () => <AppearanceSection />,
});
