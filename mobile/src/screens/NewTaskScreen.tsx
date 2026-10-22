// NewTaskScreen is M6's create-a-task form (docs/plans/active/
// mobile-create-task.md): a keyboard-safe scroll of Card sections whose
// submit runs newTask.ts's pure pipeline. Placeholder in this commit; the
// full form lands in the next one.

import { useMemo } from 'react';
import { ScrollView, StyleSheet, Text } from 'react-native';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Card } from '../ui/Card';

export function NewTaskScreen() {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={styles.content}
      contentInsetAdjustmentBehavior="automatic"
      keyboardShouldPersistTaps="handled"
    >
      <Card>
        <Text style={styles.title}>New task</Text>
      </Card>
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], gap: theme.spacing[4] },
    title: { ...theme.type.body, color: theme.foreground },
  });
}
