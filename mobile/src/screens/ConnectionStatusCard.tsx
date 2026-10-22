// ConnectionStatusCard.tsx is the card the pairing screen and the app shell
// show when a connection is waiting for approval on the daemon, was revoked,
// or failed (relay/connectionStatus.ts words them). Presentation follows the
// ZCode look: the frozen Card/StatusDot primitives, a NEUTRAL border (the old
// accent-coloured border is gone), and the action-variant / dot-tone rules
// from statusCardView.ts. The thin ReconnectingBanner shown over a live
// session lives in src/ui/Banner.tsx. Colors come from the token system only.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button } from '@expo/ui';
import { StatusView } from '../relay/connectionStatus';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Card } from '../ui/Card';
import { StatusDot } from '../ui/StatusDot';
import { TextButton } from '../ui/TextButton';
import { UiHost } from '../ui/UiHost';
import { actionVariants, statusDotTone } from './statusCardView';

export interface StatusAction {
  label: string;
  onPress: () => void;
}

export function ConnectionStatusCard({ view, actions }: { view: StatusView; actions?: StatusAction[] }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const variants = actions ? actionVariants(actions.length) : [];
  return (
    <Card style={styles.card}>
      <View style={styles.cardInner} accessibilityRole="alert">
        <View style={styles.titleRow}>
          <StatusDot tone={statusDotTone(view.tone)} />
          <Text style={styles.title}>{view.title}</Text>
        </View>
        <Text style={styles.detail}>{view.detail}</Text>
        {view.command ? (
          <Text style={styles.command} selectable>
            {view.command}
          </Text>
        ) : null}
        {variants.length > 0 && actions ? (
          <View style={styles.actions}>
            {actions.map((a, i) =>
              // vercel-react-native-skills: conditional children render the element or null, never a falsy string.
              variants[i] === 'text' ? (
                <TextButton key={a.label} label={a.label} onPress={a.onPress} />
              ) : (
                <UiHost key={a.label}>
                  <Button variant={variants[i]} label={a.label} onPress={a.onPress} style={{ height: 44 }} />
                </UiHost>
              ),
            )}
          </View>
        ) : null}
      </View>
    </Card>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { marginTop: 0 },
    cardInner: { gap: theme.spacing[2] },
    titleRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    title: {
      fontSize: theme.type.label.fontSize,
      lineHeight: theme.type.label.lineHeight,
      fontWeight: theme.type.label.fontWeight,
      color: theme.foreground,
    },
    detail: {
      fontSize: theme.type.body.fontSize,
      lineHeight: theme.type.body.lineHeight,
      color: theme.foregroundSubtle,
    },
    command: {
      fontSize: theme.type.mono.fontSize,
      lineHeight: theme.type.mono.lineHeight,
      fontFamily: theme.type.mono.fontFamily,
      color: theme.foreground,
      backgroundColor: theme.surfaceSubtle,
      borderRadius: theme.radius.md,
      padding: theme.spacing[3],
    },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: theme.spacing[2], marginTop: theme.spacing[2] },
  });
}
