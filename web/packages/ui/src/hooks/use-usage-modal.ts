import { useUsageModalStore } from "@/stores/usage-modal-store";

export function openUsageModal(): void {
  useUsageModalStore.getState().setOpen(true);
}

export function closeUsageModal(): void {
  useUsageModalStore.getState().setOpen(false);
}

export function useUsageModalOpen(): boolean {
  return useUsageModalStore((s) => s.open);
}
