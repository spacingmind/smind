// PermissionCard: a pending/resolved permission request inside the transcript.
// Options are @expo/ui Buttons in ONE UiHost row (expo-ui: native controls
// tinted by the Host); variants come from the pure permissionButtonVariants.
// Reject options use TextButton (frozen primitive: its own Host + label colour).

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button } from '@expo/ui';
import { PermissionRequestState } from '../../permissionRequests';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { permissionButtonVariants } from '../../transcript/permissionButtons';
import { Card } from '../../ui/Card';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';

export const PermissionCard = memo(function PermissionCard({
  request,
  onRespond,
}: {
  request: PermissionRequestState;
  onRespond: (requestId: string, optionId: string) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const variants = useMemo(() => permissionButtonVariants(request.options), [request.options]);
  const chosen = request.options.find((o) => o.id === request.resolvedWith?.optionId)?.label ?? request.resolvedWith?.optionId ?? '?';
  return (
    <Card style={styles.card}>
      <Text style={styles.summary} selectable>
        {request.summary}
      </Text>
      {request.status === 'pending' ? (
        <View style={styles.options}>
          {request.options.map((opt, i) =>
            variants[i] === 'text' ? (
              <TextButton key={opt.id} label={opt.label} onPress={() => onRespond(request.requestId, opt.id)} />
            ) : (
              <UiHost key={opt.id}>
                <Button
                  label={opt.label}
                  variant={variants[i]}
                  onPress={() => onRespond(request.requestId, opt.id)}
                  style={styles.button}
                />
              </UiHost>
            ),
          )}
        </View>
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
    resolved: { ...theme.type.small, color: theme.status.success },
    error: { ...theme.type.small, color: theme.status.danger },
  });
}
