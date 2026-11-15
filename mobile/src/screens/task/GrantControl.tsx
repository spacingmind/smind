// GrantControl: the scoped "Always allow in this project" control of a permission card (saved grants). It shows
// the EXACT scope before the human chooses it (this command, this project and folder, how long, which class),
// and for a command that runs project code an opt-in, UNTICKED, whose words say what it means; the save button is
// disabled until it is ticked. The daemon enforces the same rule, so this is the human's view of it, not the guard.
// Every string is drawn as plain selectable <Text>: the command is agent-authored.

import { useMemo, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import type { GrantOffer } from '../../grants/types';
import {
  GRANT_BUTTON_LABEL,
  GRANT_NEEDS_OPT_IN_HINT,
  GRANT_OPT_IN_FALLBACK,
  grantAllowProjectCode,
  grantClassLabel,
  grantNeedsOptIn,
  grantSaveEnabled,
} from '../../grants/view';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { Badge } from '../../ui/Badge';
import { OutlineButton } from '../../ui/OutlineButton';

export function GrantControl({ offer, busy, onSave }: { offer: GrantOffer; busy: boolean; onSave: (allowProjectCode: boolean) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [optIn, setOptIn] = useState(false);
  const needsOptIn = grantNeedsOptIn(offer);
  const enabled = grantSaveEnabled(offer, optIn, busy);
  return (
    <View style={styles.box}>
      <View style={styles.head}>
        <Text style={styles.title}>{GRANT_BUTTON_LABEL}</Text>
        <Badge tone={needsOptIn ? 'warning' : 'neutral'}>{grantClassLabel(offer.class)}</Badge>
      </View>
      <Text style={styles.label}>Command</Text>
      <Text style={styles.mono} selectable>
        {offer.command}
      </Text>
      <Text style={styles.label}>Project</Text>
      <Text style={styles.value} selectable>
        {offer.folder ? `${offer.project} (${offer.folder})` : offer.project}
      </Text>
      <Text style={styles.label}>Lasts</Text>
      <Text style={styles.value}>{`${offer.expiresInDays} days, or until you revoke it`}</Text>
      <Text style={styles.muted} selectable>
        {offer.note}
      </Text>
      {needsOptIn ? (
        <Pressable
          accessibilityRole="checkbox"
          accessibilityState={{ checked: optIn, disabled: busy }}
          accessibilityLabel={offer.optInLabel || GRANT_OPT_IN_FALLBACK}
          disabled={busy}
          onPress={() => setOptIn((v) => !v)}
          style={styles.optIn}
        >
          <View style={[styles.box2, optIn && styles.box2On]}>{optIn ? <AppIcon name="check" size={12} color={theme.primaryForeground} /> : null}</View>
          <Text style={styles.optInText}>{offer.optInLabel || GRANT_OPT_IN_FALLBACK}</Text>
        </Pressable>
      ) : null}
      <OutlineButton label={GRANT_BUTTON_LABEL} disabled={!enabled} onPress={() => onSave(grantAllowProjectCode(offer, optIn))} />
      {needsOptIn && !optIn && !busy ? <Text style={styles.muted}>{GRANT_NEEDS_OPT_IN_HINT}</Text> : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    box: { gap: theme.spacing[1.5], padding: theme.spacing[3], borderRadius: theme.radius.lg, borderCurve: 'continuous', borderWidth: 1, borderColor: theme.border, backgroundColor: theme.card },
    head: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    title: { ...theme.type.label, color: theme.foreground },
    label: { ...theme.type.small, color: theme.foregroundSubtle },
    value: { ...theme.type.body, color: theme.foreground },
    mono: { ...theme.type.mono, color: theme.foreground },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    optIn: { flexDirection: 'row', alignItems: 'flex-start', gap: theme.spacing[3], minHeight: 44, paddingVertical: theme.spacing[1.5] },
    box2: { width: 22, height: 22, marginTop: 1, borderRadius: theme.radius.sm, borderWidth: 1.5, borderColor: theme.foregroundSubtle, alignItems: 'center', justifyContent: 'center' },
    box2On: { backgroundColor: theme.primary, borderColor: theme.primary },
    optInText: { ...theme.type.body, color: theme.foreground, flex: 1 },
  });
}
