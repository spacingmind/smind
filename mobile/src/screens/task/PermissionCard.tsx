// PermissionCard: a pending/resolved permission request inside the transcript.
// Options are @expo/ui Buttons in ONE UiHost row (expo-ui: native controls
// tinted by the Host); variants come from the pure permissionButtonVariants.
// Reject options use TextButton (frozen primitive: its own Host + label colour).
// A pending request with a reject option also offers the optional deny reason
// (a small multi-line TextInput under the buttons: two lines at rest, growing
// to three, then scrolling, with a used/max counter near the cap); only the
// reject tap carries it -- dispatchPermissionTap keeps the two-argument call
// for the rest.

import { memo, useMemo, useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { Button } from '@expo/ui';
import { HeredocReveal } from '../../api';
import { PermissionRequestState, clampRunes, denyReasonCounter, dispatchPermissionTap } from '../../permissionRequests';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { permissionButtonVariants } from '../../transcript/permissionButtons';
import { Card } from '../../ui/Card';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';
import { HeredocReveal as HeredocRevealToggles } from './HeredocReveal';
import { PermissionPolicyLine } from './PermissionPolicyLine';

const REASON_LABEL = 'Reason for denying (optional)';

export const PermissionCard = memo(function PermissionCard({
  request,
  onRespond,
  onReveal,
}: {
  request: PermissionRequestState;
  onRespond: (requestId: string, optionId: string, reason?: string) => void;
  onReveal?: (requestId: string, index: number) => Promise<HeredocReveal>;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const variants = useMemo(() => permissionButtonVariants(request.options), [request.options]);
  const [reason, setReason] = useState('');
  const [reasonContentHeight, setReasonContentHeight] = useState(0);
  const counter = denyReasonCounter(reason);
  // The field grows with its text: two body lines at rest, capped at three
  // (plus the vertical padding); past the cap the text scrolls inside instead.
  const reasonVerticalPadding = theme.spacing[2] * 2;
  const reasonMinHeight = theme.type.body.lineHeight * 2 + reasonVerticalPadding;
  const reasonMaxHeight = theme.type.body.lineHeight * 3 + reasonVerticalPadding;
  const reasonHeight = Math.min(Math.max(reasonContentHeight + reasonVerticalPadding, reasonMinHeight), reasonMaxHeight);
  const chosen = request.options.find((o) => o.id === request.resolvedWith?.optionId)?.label ?? request.resolvedWith?.optionId ?? '?';
  const hasReject = request.options.some((o) => o.kind.startsWith('reject'));
  return (
    <Card style={styles.card}>
      <Text style={styles.summary} selectable>
        {request.summary}
      </Text>
      <PermissionPolicyLine detail={request.detail} />
      {/* The body is only revealable while the request is pending (the daemon refuses afterwards). */}
      {onReveal && request.status === 'pending' && request.detail?.heredocs ? (
        <HeredocRevealToggles refs={request.detail.heredocs} reveal={(i) => onReveal(request.requestId, i)} />
      ) : null}
      {request.status === 'pending' ? (
        <>
          <View style={styles.options}>
            {request.options.map((opt, i) =>
              variants[i] === 'text' ? (
                <TextButton key={opt.id} label={opt.label} onPress={() => dispatchPermissionTap(onRespond, request.requestId, opt.id, opt.kind, reason)} />
              ) : (
                <UiHost key={opt.id}>
                  <Button
                    label={opt.label}
                    variant={variants[i]}
                    onPress={() => dispatchPermissionTap(onRespond, request.requestId, opt.id, opt.kind, reason)}
                    style={styles.button}
                  />
                </UiHost>
              ),
            )}
          </View>
          {hasReject ? (
            <View style={styles.reasonField}>
              <Text style={styles.reasonLabel}>{REASON_LABEL}</Text>
              <TextInput
                style={[styles.reasonInput, { height: reasonHeight }]}
                value={reason}
                onChangeText={(next) => setReason(clampRunes(next))}
                placeholder="Tell the agent what to do instead"
                placeholderTextColor={theme.foregroundSubtlest}
                multiline
                numberOfLines={2}
                blurOnSubmit={false}
                onContentSizeChange={(e) => setReasonContentHeight(e.nativeEvent.contentSize.height)}
                accessibilityLabel={REASON_LABEL}
              />
              {counter.shown ? (
                <Text style={[styles.reasonCounter, counter.atLimit ? styles.reasonCounterAtLimit : null]}>
                  {counter.used} / {counter.max}
                  {counter.atLimit ? ' — limit reached' : ''}
                </Text>
              ) : null}
            </View>
          ) : null}
        </>
      ) : (
        <Text style={styles.resolved} selectable>
          {request.resolvedWith?.by === 'tap' ? 'chosen: ' : 'resolved: '}
          {chosen}
        </Text>
      )}
      {request.error !== null ? (
        <Text style={styles.error} selectable>
          Couldn't respond: {request.error}
        </Text>
      ) : null}
    </Card>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { backgroundColor: theme.waiting.surface, gap: theme.spacing[3] },
    summary: { ...theme.type.body, color: theme.foreground },
    options: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    button: { height: 44 },
    reasonField: { gap: theme.spacing[1.5] },
    reasonLabel: { ...theme.type.label, color: theme.foregroundSubtle },
    reasonInput: {
      ...theme.type.body,
      color: theme.foreground,
      backgroundColor: theme.input.background,
      borderWidth: 1,
      borderColor: theme.input.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      paddingHorizontal: theme.spacing[3],
      paddingVertical: theme.spacing[2],
    },
    reasonCounter: { ...theme.type.small, color: theme.foregroundSubtle, alignSelf: 'flex-end' },
    reasonCounterAtLimit: { color: theme.status.danger },
    resolved: { ...theme.type.small, color: theme.status.success },
    error: { ...theme.type.small, color: theme.status.danger },
  });
}
