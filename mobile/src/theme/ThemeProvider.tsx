// ThemeProvider.tsx is theme.ts's react-native-dependent half: the
// AppThemeProvider/useAppTheme pair, kept out of theme.ts so that file
// stays importable by vitest's plain (non-Metro) transform, which can't
// parse react-native's Flow syntax (see theme.ts's header comment).
// Screens import useAppTheme from here; App.tsx mounts AppThemeProvider
// once at the root.

import { createContext, ReactNode, useContext, useMemo } from 'react';
import { useColorScheme } from 'react-native';
import { AppTheme, lightTheme, resolveAppTheme } from '../theme';

const AppThemeContext = createContext<AppTheme>(lightTheme);

export function AppThemeProvider({ children }: { children: ReactNode }) {
  const scheme = useColorScheme();
  const theme = useMemo(() => resolveAppTheme(scheme), [scheme]);
  return <AppThemeContext.Provider value={theme}>{children}</AppThemeContext.Provider>;
}

export function useAppTheme(): AppTheme {
  return useContext(AppThemeContext);
}
