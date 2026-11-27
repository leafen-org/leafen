import { settlingPage, useSettlingSuggestions } from "@/lib/suggestionSettling";

// A suggestion accepted or rejected from here draws its words and blocks
// settled at the press, before the server's text and blocks come
// (suggestionSettling.ts), as Notion's page changes with the press: words
// suggested in or restyled and accepted, or suggested away and rejected,
// look as any other; words suggested away and accepted, or suggested in and
// rejected, are gone, and so are blocks suggested away and accepted, or
// suggested in and rejected, with the "⏎" a block suggested in ends the line
// before in; an accepted break loses its "⏎" too, and a list item suggested
// in and accepted its blue marker. A rejected restyle keeps
// its look until the server's text brings back the old one, and a rejected
// break its "⏎" until the server joins its lines. These come after the open
// card's marks (CommentAnchorMarks), which they replace.

const KEPT = "color: inherit; background: none; border-bottom: none; padding-bottom: 0;";
const KEPT_REMOVAL = `${KEPT} text-decoration: none;`;
const GONE = "display: none;";
const NO_BREAK = "content: none;";
// A list marker's own color (EditorText.module.css .bulletDot), over the suggested block's blue.
const MARKER = "color: var(--block-color, var(--text-default));";

export function SettlingSuggestionMarks({ pageId }: { pageId: string }) {
  const settling = useSettlingSuggestions(pageId);
  if (!settling.size) return null;
  const page = settlingPage(pageId);
  const words = (id: string, kind: string) => `${page} [data-rt-editable] [data-suggestion-id="${CSS.escape(id)}"][data-suggestion="${kind}"]`;
  // The "⏎" stands after a line's words, or alone in an empty line (EditorInlineDecorations.module.css).
  const lineBreak = (id: string, more = "") => {
    const line = `${page} [data-rt-editable][data-suggested-break="true"][data-suggested-break-by="${CSS.escape(id)}"]${more}`;
    return [`${line} > [data-node-view-content-react]::after`, `${line} > [data-node-view-content-react]::before`];
  };
  const kept: string[] = [], keptRemovals: string[] = [], gone: string[] = [], breaks: string[] = [], markers: string[] = [];
  for (const [id, { decision, removed }] of settling) {
    if (decision === "accept") {
      kept.push(words(id, "insert"), words(id, "format"));
      gone.push(words(id, "delete"), ...removed.map((blockId) => `${page}[data-block-id="${CSS.escape(blockId)}"]`));
      breaks.push(...lineBreak(id));
      // Named by all it is, to outweigh the suggested block's marker rule (EditorInlineDecorations.module.css).
      markers.push(`${page}[data-suggested-by="${CSS.escape(id)}"] [data-list-marker][aria-hidden="true"][contenteditable="false"]`);
    } else {
      gone.push(words(id, "insert"), `${page}[data-suggested-by="${CSS.escape(id)}"]`);
      keptRemovals.push(words(id, "delete"));
      breaks.push(...lineBreak(id, ":not([data-suggested-split])"));
    }
  }
  const rule = (selectors: readonly string[], style: string) => selectors.length ? `${selectors.join(",\n")} { ${style} }` : "";
  return <style data-settling-suggestion-marks>{[rule(kept, KEPT), rule(keptRemovals, KEPT_REMOVAL), rule(gone, GONE), rule(breaks, NO_BREAK), rule(markers, MARKER)].join("\n")}</style>;
}
