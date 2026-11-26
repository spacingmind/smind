/**
 * The daemon words an error as `<rpc method>: <package>: <sentence>` (e.g. `browser.settings.set: browser: allowlist
 * entry "*.x" must be one exact host...`). The person reads a sentence: the leading method and package labels are
 * internal and are dropped, the first letter is capitalised and a full stop is added when the text has none. The words
 * in between are the daemon's, unchanged (the CLI keeps printing the daemon's text as it is).
 */

const METHOD_LABEL = /^[A-Za-z]+(?:\.[A-Za-z]+)+: /;

/** The daemon's error text as one sentence a person reads. */
export function daemonSentence(message: string): string {
  let bare = message.trim();
  const labelled = METHOD_LABEL.test(bare);
  bare = bare.replace(METHOD_LABEL, "");
  if (labelled || /^browser: /.test(bare)) bare = bare.replace(/^[a-z]+: /, "");
  bare = bare.trim();
  if (bare === "") return bare;
  const capital = bare.charAt(0).toUpperCase() + bare.slice(1);
  return /[.!?]$/.test(capital) ? capital : `${capital}.`;
}
