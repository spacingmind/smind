// Skeleton.tsx is the static loading placeholder block (M4's loading state):
// a quiet surfaceSubtle rectangle, deliberately NOT animated -- a shimmer
// would need an animation loop on every mount, and a static block already
// communicates "content is coming" without stealing attention.

import { StyleProp, StyleSheet, View, ViewStyle } from 'react-native';
import { useAppTheme } from '../theme/ThemeProvider';

export type SkeletonRadius = 'sm' | 'md' | 'lg' | 'xl' | '2xl' | 'full';

export function Skeleton({
  width = '100%',
  height = 16,
  radius = 'md',
  style,
}: {
  width?: number | `${number}%` | '100%';
  height?: number;
  radius?: SkeletonRadius;
  style?: StyleProp<ViewStyle>;
}) {
  const theme = useAppTheme();
  return (
    <View
      style={[
        {
          width,
          height,
          borderRadius: theme.radius[radius],
          borderCurve: 'continuous',
          backgroundColor: theme.surfaceSubtle,
        },
        style,
      ]}
    />
  );
}
