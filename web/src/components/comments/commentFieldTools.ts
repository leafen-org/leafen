/**
 * When a new comment's field shows its tools (📎, @ and send): while it has
 * the focus or holds a draft — words or files. An empty field left alone
 * shows none, though it had the focus before or a comment was sent from it,
 * as Notion's database page discussion (seen 2026-11-25 in Chrome at 1440,
 * two tabs: none until clicked, gone again once left empty, kept while a
 * word stays in it). The layout builder's preview of that discussion draws
 * such a field, never focused nor typed in, so it shows none either (Notion's
 * builder keeps them out of sight there too, seen the same day).
 */
export function commentFieldToolsShown(field: { focused: boolean; draftEmpty: boolean; files: number }): boolean {
  return field.focused || !field.draftEmpty || field.files > 0;
}

/** The field a preview draws: not focused, nothing typed, no files. */
export const PREVIEW_COMMENT_FIELD = { focused: false, draftEmpty: true, files: 0 } as const;
