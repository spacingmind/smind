// FooterBanner: the needs-continue banner (ADR-0027 D13) -- the warning
// sentence from describeRunFooter and a filled Continue button. The screen
// decides visibility; this only renders.

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button } from '@expo/ui';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Card } from '../../ui/Card';
import { StatusDot } from '../../ui/StatusDot';
import { UiHost } from '../../ui/UiHost';

export const FooterBanner = memo(function FooterBanner({ sentence, note, waiting, onContinue }: { sentence: string; note?: string; waiting?: boolean; onContinue: () => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <Card tone="subtle" style={styles.card}>
      <View style={styles.sentenceRow}>
        <StatusDot tone="warning" />
        <Text style={styles.sentence} selectable>
          {sentence}
        </Text>
      </View>
      {note ? (
        <Text style={styles.note} selectable>
          {note}
        </Text>
      ) : null}
      {waiting ? null : ( // a held retry is already queued: Continue would send a second one
        <UiHost style={styles.host}>
          <Button label="Continue" variant="filled" onPress={onContinue} style={styles.button} />
        </UiHost>
      )}
    </Card>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { gap: theme.spacing[3] },
    sentenceRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    sentence: { ...theme.type.body, color: theme.foreground, flex: 1 },
    note: { ...theme.type.small, color: theme.foregroundSubtle },
    host: { alignSelf: 'flex-start' },
    button: { height: 44 },
  });
}
