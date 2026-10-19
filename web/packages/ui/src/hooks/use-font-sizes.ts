import { useCallback, useEffect } from "react";

import { applyFontSizes, type FontSizes, type FontSizeStep } from "@/lib/settings-preferences";
import { useFontSizesStore } from "@/stores/font-sizes-store";

/**
 * Owns the three Appearance font-size steps: reads the persisted value
 * from the shared store, applies it to the document on every change
 * (including the very first render, so a value that drifted from what's
 * already on the document -- there's no pre-paint bootstrap script for
 * this the way lib/theme.ts's flash-prevention one exists for dark mode,
 * since a wrong font size for one frame is not the same class of problem
 * as a flash-of-wrong-theme) -- gets reconciled immediately.
 */
export function useFontSizes(): {
  fontSizes: FontSizes;
  setFontSize: (axis: keyof FontSizes, step: FontSizeStep) => void;
} {
  const fontSizes = useFontSizesStore((s) => s.value);

  useEffect(() => {
    applyFontSizes(fontSizes);
  }, [fontSizes]);

  const setFontSize = useCallback((axis: keyof FontSizes, step: FontSizeStep) => {
    const { value: prev, set } = useFontSizesStore.getState();
    set({ ...prev, [axis]: step });
  }, []);

  return { fontSizes, setFontSize };
}
