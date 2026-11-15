/** An error from Rust is a bare phrase ("the notification could not be sent"); shown to a person it is a sentence. */
export function asSentence(message: string): string {
  const text = message.trim();
  if (text === "") return text;
  const capitalised = text.charAt(0).toUpperCase() + text.slice(1);
  return /[.!?]$/.test(capitalised) ? capitalised : `${capitalised}.`;
}
