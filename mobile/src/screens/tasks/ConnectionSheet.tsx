// ConnectionSheet is the header "Connection" action's native sheet: the
// connection status and this device's label as two short static rows, and a
// danger TextButton that disconnects and closes the sheet. All colours come
// from tokens (expo-design-system); the sheet itself is the @expo/ui
// BottomSheet driven by isPresented/onDismiss (NOT isOpen/onChange), painted
// with the elevated `overlay` token via containerColor -- in dark mode an
// elevated surface is LIGHTER than the screen behind it, never pure black.
// The rows are plain RN views (BottomSheet children are RN views on every
// platform): the universal @expo/ui ListItem always renders a focusable
// Pressable, which put a keyboard-focus ring on a row that does nothing.

import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { BottomSheet } from '@expo/ui';
import { ConnectionState } from '../../relay/RelayConnection';
import { describeState } from '../../relay/connectionStatus';
import { defaultDeviceLabel } from '../../relay/platformKeyStore';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';

interface Props {
  open: boolean;
  onClose: () => void;
  onDisconnect: () => void;
  state: ConnectionState;
}

/** "Connected" when live, else the status sentence the connection screens show. */
function statusText(state: ConnectionState): string {
  if (state.kind === 'connected') return 'Connected';
  const view = describeState(state);
  if (view) return view.title;
  return 'Reconnecting…';
}

export function ConnectionSheet({ open, onClose, onDisconnect, state }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  // defaultDeviceLabel is sync today but may go async (it reads the
  // platform): load it in an effect, "…" until it resolves.
  const [deviceLabel, setDeviceLabel] = useState('…');
  useEffect(() => {
    let alive = true;
    Promise.resolve(defaultDeviceLabel()).then((label) => {
      if (alive) setDeviceLabel(label);
    });
    return () => {
      alive = false;
    };
  }, []);

  return (
    <UiHost>
      <BottomSheet isPresented={open} onDismiss={onClose} snapPoints={['half']} containerColor={theme.overlay}>
        <View style={styles.content}>
          <View>
            <View style={[styles.row, styles.rowDivider]}>
              <Text style={styles.rowLabel}>Status</Text>
              <Text style={styles.rowValue} selectable>
                {statusText(state)}
              </Text>
            </View>
            <View style={styles.row}>
              <Text style={styles.rowLabel}>Device</Text>
              <Text style={styles.rowValue} selectable>
                {deviceLabel}
              </Text>
            </View>
          </View>
          <TextButton label="Disconnect" tone="danger" onPress={() => { onClose(); onDisconnect(); }} style={styles.disconnect} />
        </View>
      </BottomSheet>
    </UiHost>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    content: {
      gap: theme.spacing[4],
      paddingBottom: theme.spacing[4],
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      gap: theme.spacing[3],
      minHeight: 44,
      paddingHorizontal: theme.spacing[4],
    },
    rowDivider: { borderBottomWidth: 1, borderBottomColor: theme.border },
    rowLabel: { ...theme.type.body, color: theme.foreground },
    rowValue: { ...theme.type.label, color: theme.foregroundSubtle, flexShrink: 1, textAlign: 'right' },
    disconnect: { alignSelf: 'flex-start' },
  });
}
