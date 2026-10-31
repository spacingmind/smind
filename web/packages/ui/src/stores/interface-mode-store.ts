import { readInterfaceMode, writeInterfaceMode, type InterfaceMode } from "@/lib/interface-mode";
import { createPreferenceStore } from "@/stores/create-preference-store";

/** The interface mode (ADR-0049), shared by every surface that branches on it; other tabs follow via the storage event. */
export const useInterfaceModeStore = createPreferenceStore<InterfaceMode>(readInterfaceMode, writeInterfaceMode);
