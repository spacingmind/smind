/**
 * Tidies release-please's CHANGELOG section for the desktop update prompt:
 * the first heading (`## [0.11.0](compare-url) (date)`) repeats what the
 * dialog title already says, and every bullet ends in a commit-hash link
 * (`([<sha>](…/commit/…))`) that means nothing to a reader. PR links
 * (`([#NNN](…))`) stay.
 */
export function tidyReleaseNotes(notes: string): string {
  return notes
    .replace(/^\s*#{1,2} \[?v?\d+\.\d+\.\d+[^\n]*\n/, "")
    .replace(/ \(\[[0-9a-f]{7,40}\]\([^)]*\/commit\/[0-9a-f]+\)\)/g, "")
    .trim();
}
