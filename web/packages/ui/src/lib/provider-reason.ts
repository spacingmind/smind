const MAX_REASON_LENGTH = 80;

/** Short human reason for a provider's raw (often Go) unavailable error; keep the raw text for a tooltip. */
export function formatProviderReason(raw: string): string {
  if (raw.includes("executable file not found")) return "Not installed";
  if (raw.includes("directory does not exist")) return "Not set up";
  if (raw.startsWith("no account configured")) return "No account";
  if (raw.startsWith("found ") && /too old|needs|requires|version/.test(raw)) return "Update needed";
  const text = raw.trim();
  const sentence = text.split(/(?<=[.!?])\s/)[0].replace(/\.$/, "");
  return sentence.length > MAX_REASON_LENGTH ? `${sentence.slice(0, MAX_REASON_LENGTH - 1)}…` : sentence;
}
