import "./prismManual";
import * as Prism from "prismjs";
import type { TextSpan } from "@/lib/types";
import { spanToHtml } from "./richtext";
import { escapeHtml } from "./richtextHtmlEscaping";

// Notion draws each piece of a block as a Prism token with its own names and
// those of the tokens around it (seen 2026-11-01: "token punctuation
// namespace import"), and Prism's themes color them by those names
// (codeTheme.css), the later rule winning where several match.
function html(stream: Prism.TokenStream, names: readonly string[], write: (text: string) => string): string {
  if (typeof stream === "string") {
    return names.length ? `<span class="token ${names.join(" ")}">${write(stream)}</span>` : write(stream);
  }
  if (Array.isArray(stream)) return stream.map((part) => html(part, names, write)).join("");
  const own = [stream.type, ...[stream.alias ?? []].flat()].filter((name) => /^[\w-]+$/u.test(name));
  return html(stream.content, [...own, ...names], write);
}

/**
 * Writes the code's pieces, given in order, each with the marks its characters
 * carry, inside the piece's token so a word's own color wins over the token's.
 * A link the code's words keep is not drawn, as Notion's code draws none
 * (seen 2026-11-14: "const" linked by ⌘K kept its address, read through the
 * API, and showed as code, with no underline or card).
 */
export function markedCodeWriter(spans: readonly TextSpan[]) {
  let index = 0, used = 0;
  return (text: string) => {
    let written = "", rest = text;
    while (rest) {
      const span = spans[index];
      if (!span) return written + escapeHtml(rest);
      const room = span.text.length - used;
      if (room <= 0) { index += 1; used = 0; continue; }
      const piece = rest.slice(0, room);
      written += spanToHtml({ ...span, link: undefined, text: piece });
      used += piece.length;
      rest = rest.slice(piece.length);
    }
    return written;
  };
}

/** A code block's source colored as Notion's is, by a Prism grammar. */
export function prismHighlightHtml(code: string, grammar: Prism.Grammar, spans: readonly TextSpan[]) {
  return html(Prism.tokenize(code, grammar), [], markedCodeWriter(spans));
}
