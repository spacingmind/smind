// ConnectionSheet is the header "Connection" action's native sheet: the
// connection status and this device's label as short fixed @expo/ui List
// rows (expo-ui: List is right only for short fixed lists like these), and
// a danger TextButton that disconnects and closes the sheet. All colours
// come from tokens (expo-design-system); the sheet itself is the frozen
// @expo/ui BottomSheet driven by isPresented/onDismiss (NOT isOpen/onChange).

import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { BottomSheet, List, ListItem } from '@expo/ui';
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
      <BottomSheet isPresented={open} onDismiss={onClose} snapPoints={['half']}>
        <View style={styles.content}>
          <List>
            <ListItem>
              <Text style={styles.rowLabel}>Status</Text>
              <Text style={styles.rowValue}>{statusText(state)}</Text>
            </ListItem>
            <ListItem>
              <Text style={styles.rowLabel}>Device</Text>
              <Text style={styles.rowValue}>{deviceLabel}</Text>
            </ListItem>
          </List>
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
    rowLabel: { ...theme.type.small, color: theme.foregroundSubtle },
    rowValue: { ...theme.type.label, color: theme.foreground },
    disconnect: { alignSelf: 'flex-start' },
  });
}
