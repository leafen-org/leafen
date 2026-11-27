import type { GraphBlock, GraphBlockType } from "@/contentGraph";
import type { TextSpan } from "@/lib/types";

export const captionBlockTypes: ReadonlySet<GraphBlockType> = new Set<GraphBlockType>([
  "image", "video", "audio", "file", "pdf", "bookmark", "embed", "code",
]);

export function singleLineCaptionSpans(spans: TextSpan[]) {
  return spans.map(span => /[\r\n]/u.test(span.text)
    ? { ...span, text: span.text.replace(/\s*[\r\n]+\s*/gu, " ") } : span);
}

export function blockCaptionSpans(block: GraphBlock): TextSpan[] {
  return Array.isArray(block.content.caption) ? block.content.caption as TextSpan[] : [];
}

/** A block waiting for its file or address shows no caption and its menu
 *  offers none, though its caption is kept, as Notion's: a web bookmark or
 *  embed (seen 2026-11-10: one made through the API with a caption), and a
 *  file, PDF, picture, video or audio file (seen 2026-11-11: one page
 *  Markdown left empty of an address it doesn't take, caption and all). */
export function blockCaptionHeld(block: GraphBlock) {
  if (!captionBlockTypes.has(block.type)) return false;
  return block.type === "code" || Boolean(String(block.content.url ?? "").trim() || block.content.fileId);
}

export function blockCaptionShown(block: GraphBlock) {
  return blockCaptionHeld(block) && (block.content.captionVisible === true
    || blockCaptionSpans(block).some(span => span.text.length > 0));
}

/** The caption field's name, by its block's kind (blockItem catalog keys). */
export function captionLabelKey(type: GraphBlockType | undefined) {
  switch (type) {
    case "image": return "image.caption";
    case "video": return "media.videoCaption";
    case "audio": return "media.audioCaption";
    case "file": case "pdf": return "file.caption";
    case "code": return "code.caption";
    default: return "embed.caption";
  }
}
