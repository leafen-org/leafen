import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { TEXT_BLOCK_TYPES } from "@/lib/blockSemantics";
import { editorBlockChildren } from "./editorBlockIndex";

/**
 * The line a press under a page's blocks puts its caret in, at its end, or
 * null where it makes a new line instead: the last line when it is an empty
 * paragraph, as Notion's everywhere; and on a phone, from a finger, the last
 * line drawn whatever its words — the page's last block, into its last child
 * while it shows its children —, with no new line made, as Notion's Android
 * app (seen 2026-11-21 on a Galaxy A90, w2270, twice: a tap under the body
 * put the caret after the last line's last letter and raised the keyboard;
 * its phone web made a new line). A page ending in what holds no words (a
 * picture, a database) still gets a new line under it: the app's is not seen.
 */
export function pageEndLine(graph: ContentGraph, text: (block: GraphBlock) => string, phoneFinger: boolean): string | null {
  let last = editorBlockChildren(graph, null).at(-1);
  if (!phoneFinger) {
    return last?.type === "paragraph" && !text(last) && editorBlockChildren(graph, last.id).length === 0 ? last.id : null;
  }
  while (last && last.content.collapsed !== true) {
    const child: GraphBlock | undefined = editorBlockChildren(graph, last.id).at(-1);
    if (!child) break;
    last = child;
  }
  return last && TEXT_BLOCK_TYPES.has(last.type) ? last.id : null;
}
