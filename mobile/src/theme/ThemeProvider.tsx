// ThemeProvider.tsx is theme.ts's react-native-dependent half: the
// AppThemeProvider/useAppTheme pair, kept out of theme.ts so that file
// stays importable by vitest's plain (non-Metro) transform, which can't
// parse react-native's Flow syntax (see theme.ts's header comment).
// Screens import useAppTheme from here; App.tsx mounts AppThemeProvider
// once at the root.

import { createContext, ReactNode, useContext, useMemo, useSyncExternalStore } from 'react';
import { useColorScheme } from 'react-native';
import { applyPluginTheme } from '../plugins/themeMap';
import { activeTheme, pluginThemeStore } from '../plugins/themeStore';
import { AppTheme, lightTheme, resolveAppTheme } from '../theme';

const AppThemeContext = createContext<AppTheme>(lightTheme);

export function AppThemeProvider({ children }: { children: ReactNode }) {
  const scheme = useColorScheme();
  // ADR-0042 plugin theme: the app's own colours for the scheme, with the selected plugin theme's laid over them (or not at all).
  const plugin = useSyncExternalStore(pluginThemeStore.subscribe, pluginThemeStore.get);
  const theme = useMemo(
    () => applyPluginTheme(resolveAppTheme(scheme), activeTheme(plugin), scheme === 'dark' ? 'dark' : 'light'),
    [scheme, plugin],
  );
  return <AppThemeContext.Provider value={theme}>{children}</AppThemeContext.Provider>;
}

export function useAppTheme(): AppTheme {
  return useContext(AppThemeContext);
}
