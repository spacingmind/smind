// SectionHeader is the micro-caps label above one grouped card list.

import { useMemo } from 'react';
import { StyleSheet, Text } from 'react-native';
import { useAppTheme } from '../../theme/ThemeProvider';

export function SectionHeader({ title }: { title: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return <Text style={styles.header}>{title}</Text>;
}

function makeStyles(theme: ReturnType<typeof useAppTheme>) {
  return StyleSheet.create({
    header: {
      ...theme.type.micro,
      color: theme.foregroundSubtle,
      textTransform: 'uppercase',
      letterSpacing: 0.5,
      paddingTop: theme.spacing[4],
      marginBottom: theme.spacing[2],
    },
  });
}
