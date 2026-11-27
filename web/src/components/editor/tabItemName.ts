import type { GraphBlock } from "@/contentGraph";
import { yTextToRichText } from "@/lib/collaborationCrdtCodec";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/**
 * A tab's name as Notion's tab shows it (seen 2026-11-14): its item's text,
 * an inline equation as its expression between dollar signs ("$x$", "$$" for
 * an empty one), an equation item its expression. A name of nothing — an
 * empty paragraph, a callout, a divider — shows the new tab's name.
 */
export function tabItemName(adapter: NativeEditorGraphAdapter, item: GraphBlock) {
  if (item.type === "equation") return typeof item.content.expression === "string" ? item.content.expression : "";
  const text = adapter.collaborationDocument?.getText(`block:${item.id}`);
  const spans = text ? yTextToRichText(text) : undefined;
  if (!spans) return adapter.text(item);
  return spans.map((span) => span.emptyPiece === "equation" ? "$$"
    : typeof span.equation === "string" ? `$${span.equation || span.text}$`
    : span.text).join("");
}
