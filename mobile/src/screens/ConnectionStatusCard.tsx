// ConnectionStatusCard.tsx is the full-width card the pairing screen and the
// app shell show when a connection is waiting for approval on the daemon, was
// revoked, or failed (relay/connectionStatus.ts words them), and the thin
// banner shown over a live session while it reconnects. Colors come from the
// token system only.

import { ReactNode, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button } from '@expo/ui';
import { StatusView } from '../relay/connectionStatus';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { UiHost } from '../ui/UiHost';

export interface StatusAction {
  label: string;
  onPress: () => void;
}

export function ConnectionStatusCard({ view, actions }: { view: StatusView; actions?: StatusAction[] }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const accent = theme.status[view.tone === 'info' ? 'running' : view.tone === 'warning' ? 'warning' : 'danger'];
  return (
    <View style={[styles.card, { borderColor: accent }]} accessibilityRole="alert">
      <View style={styles.titleRow}>
        <View style={[styles.dot, { backgroundColor: accent }]} />
        <Text style={styles.title}>{view.title}</Text>
      </View>
      <Text style={styles.detail}>{view.detail}</Text>
      {view.command ? (
        <Text style={styles.command} selectable>
          {view.command}
        </Text>
      ) : null}
      {actions && actions.length > 0 ? (
        <View style={styles.actions}>
          {actions.map((a) => (
            <UiHost key={a.label}>
              <Button label={a.label} onPress={a.onPress} />
            </UiHost>
          ))}
        </View>
      ) : null}
    </View>
  );
}

/** A thin strip over a live session while the connection recovers. */
export function ReconnectingBanner({ children }: { children: ReactNode }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.banner} accessibilityRole="alert" accessibilityLiveRegion="polite">
      <Text style={styles.bannerText}>{children}</Text>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: {
      borderWidth: 1,
      borderRadius: theme.radius.md,
      backgroundColor: theme.surface[1],
      padding: theme.spacing[4],
      marginTop: theme.spacing[4],
      gap: theme.spacing[2],
    },
    titleRow: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    dot: { width: 6, height: 6, borderRadius: theme.radius.full },
    title: {
      fontSize: theme.type.panelTitle.fontSize,
      lineHeight: theme.type.panelTitle.lineHeight,
      fontWeight: theme.type.panelTitle.fontWeight,
      color: theme.foreground,
    },
    detail: {
      fontSize: theme.type.interface.fontSize,
      lineHeight: theme.type.interface.lineHeight,
      color: theme.foregroundMuted,
    },
    command: {
      fontSize: theme.type.codeAnnotation.fontSize,
      lineHeight: theme.type.codeAnnotation.lineHeight,
      fontFamily: theme.type.codeAnnotation.fontFamily,
      color: theme.foreground,
      backgroundColor: theme.surface[2],
      borderRadius: theme.radius.sm,
      padding: theme.spacing[3],
    },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: theme.spacing[2], marginTop: theme.spacing[2] },
    banner: {
      backgroundColor: theme.surface[2],
      borderBottomWidth: 1,
      borderBottomColor: theme.border,
      paddingVertical: theme.spacing[2],
      paddingHorizontal: theme.spacing[4],
    },
    bannerText: {
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      fontWeight: theme.type.metadataLabel.fontWeight,
      color: theme.foregroundMuted,
    },
  });
}
