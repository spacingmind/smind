// PermissionCard: a pending/resolved permission request inside the transcript.
// Options are @expo/ui Buttons in ONE UiHost row (expo-ui: native controls
// tinted by the Host); variants come from the pure permissionButtonVariants.
// Reject options use TextButton (frozen primitive: its own Host + label colour).
// A pending request with a reject option also offers the optional deny reason,
// behind a TextButton ("Deny with reason"): the multi-line TextInput (two lines
// at rest, growing to three, then scrolling, with a used/max counter near the
// cap) appears only while the toggle is open, its text survives a collapse, and
// only the reject tap carries it -- dispatchPermissionTap keeps the
// two-argument call for the rest.

import { memo, useMemo, useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { Button } from '@expo/ui';
import { HeredocReveal } from '../../api';
import { errorCopy, resolvedCopy, showReasonField, showReasonToggle } from '../../permissionCardView';
import { PermissionRequestState, clampRunes, denyReasonCounter, dispatchPermissionTap } from '../../permissionRequests';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { permissionButtonVariants } from '../../transcript/permissionButtons';
import { isLongOptionLabel } from '../../transcript/permissionLabels';
import { Card } from '../../ui/Card';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';
import { HeredocReveal as HeredocRevealToggles } from './HeredocReveal';
import { GrantControl } from './GrantControl';
import { PermissionPolicyLine } from './PermissionPolicyLine';

const REASON_LABEL = 'Reason for denying (optional)';

export const PermissionCard = memo(function PermissionCard({
  request,
  onRespond,
  onReveal,
  onAlwaysAllow,
}: {
  request: PermissionRequestState;
  onRespond: (requestId: string, optionId: string, reason?: string) => void;
  /** Saves the request as a project grant too; offered only when the daemon sent `detail.grant`. */
  onAlwaysAllow?: (requestId: string, allowProjectCode: boolean) => void;
  onReveal?: (requestId: string, index: number) => Promise<HeredocReveal>;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const variants = useMemo(
    () => permissionButtonVariants(request.options, request.detail?.kind),
    [request.options, request.detail?.kind],
  );
  const [reason, setReason] = useState('');
  const [reasonOpen, setReasonOpen] = useState(false);
  const [reasonContentHeight, setReasonContentHeight] = useState(0);
  const counter = denyReasonCounter(reason);
  // The field grows with its text: two body lines at rest, capped at three
  // (plus the vertical padding); past the cap the text scrolls inside instead.
  const reasonVerticalPadding = theme.spacing[2] * 2;
  const reasonMinHeight = theme.type.body.lineHeight * 2 + reasonVerticalPadding;
  const reasonMaxHeight = theme.type.body.lineHeight * 3 + reasonVerticalPadding;
  const reasonHeight = Math.min(Math.max(reasonContentHeight + reasonVerticalPadding, reasonMinHeight), reasonMaxHeight);
  const hasReject = request.options.some((o) => o.kind.startsWith('reject'));
  const error = request.error !== null ? errorCopy(request.error) : null;
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
      {error !== null ? (
        <View style={styles.errorField}>
          <Text style={styles.error} selectable>
            {error.headline}
          </Text>
          <Text style={styles.errorDetail} selectable>
            {error.detail}
          </Text>
        </View>
      ) : null}
      {request.status === 'pending' ? (
        <>
          <View style={styles.options}>
            {request.options.map((opt, i) => {
              // A long label gets its own full-width row; the button then grows
              // past the fixed height instead of being pinned to it. minHeight
              // is not in @expo/ui's UniversalStyle, so it rides the UiHost.
              const long = isLongOptionLabel(opt.label);
              const onPress = () => dispatchPermissionTap(onRespond, request.requestId, opt.id, opt.kind, reason);
              if (variants[i] === 'text') {
                return long ? (
                  <View key={opt.id} style={styles.optionWide}>
                    <TextButton label={opt.label} onPress={onPress} />
                  </View>
                ) : (
                  <TextButton key={opt.id} label={opt.label} onPress={onPress} />
                );
              }
              return long ? (
                <View key={opt.id} style={styles.optionWide}>
                  <UiHost style={styles.buttonWide}>
                    <Button label={opt.label} variant={variants[i]} onPress={onPress} />
                  </UiHost>
                </View>
              ) : (
                <UiHost key={opt.id}>
                  <Button label={opt.label} variant={variants[i]} onPress={onPress} style={styles.button} />
                </UiHost>
              );
            })}
            {showReasonToggle(hasReject) ? (
              <TextButton label={reasonOpen ? 'Hide reason' : 'Deny with reason'} onPress={() => setReasonOpen((open) => !open)} />
            ) : null}
          </View>
          {request.detail?.grant && !request.detail.gitRule && onAlwaysAllow && request.options.some((o) => o.kind === 'allow_once') ? (
            <GrantControl offer={request.detail.grant} busy={false} onSave={(allowProjectCode) => onAlwaysAllow(request.requestId, allowProjectCode)} />
          ) : null}
          {showReasonField({ hasReject, expanded: reasonOpen }) ? (
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
          {resolvedCopy({
            by: request.resolvedWith?.by,
            reason: request.resolvedWith?.reason,
            optionKind: request.options.find((o) => o.id === request.resolvedWith?.optionId)?.kind,
          })}
        </Text>
      )}
    </Card>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { backgroundColor: theme.waiting.surface, gap: theme.spacing[3] },
    summary: { ...theme.type.body, color: theme.foreground },
    options: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    optionWide: { width: '100%' },
    button: { height: 44 },
    buttonWide: { minHeight: 44, alignSelf: 'stretch', width: '100%' },
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
    errorField: { gap: theme.spacing[1] },
    error: { ...theme.type.small, color: theme.status.danger },
    errorDetail: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}
