/** "88 KiB" / "10 MiB": the units the daemon's caps are stated in (ADR-0038). */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kib = bytes / 1024;
  if (kib < 1024) return `${kib < 10 ? kib.toFixed(1) : Math.round(kib)} KiB`;
  const mib = kib / 1024;
  return `${mib < 10 && !Number.isInteger(mib) ? mib.toFixed(1) : Math.round(mib)} MiB`;
}
