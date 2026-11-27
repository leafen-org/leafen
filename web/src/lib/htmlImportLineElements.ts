import { WORDS_GAP } from "./htmlImportSpacing";

/**
 * Elements of an imported HTML document that is no export which Notion's
 * HTML import reads as a line or a block of their own in a block's words
 * (seen 2026-11-13). A script, a text area, an SVG drawing and a canvas
 * (LINE) are a line of their own holding a text area's or a drawing's words:
 * in a paragraph and in the page's body they end it, their words a paragraph
 * between ("<p>앞 <textarea>글</textarea> 뒤</p>" is "앞", "글" and "뒤"),
 * wherever they stand in its formatting; in a <div> or a table cell they are
 * a line of their own, an empty one when they hold no words ("앞\n\n뒤"); in
 * a list item an empty line, their words a paragraph under it, a script a
 * line break; in a heading their words in the line, the spaces around them
 * left out. A picture, a frame and a video (MEDIA) end a paragraph and a
 * <div>, right in it or in a <span>, a picture's or a frame's block between
 * (a picture in bold or a link stays in the words, its block after them); in
 * a list item and a table cell a frame and a video are an empty line, in a
 * heading the spaces a missing element leaves.
 */
const LINE_TAGS = new Set(["SCRIPT", "TEXTAREA", "SVG", "CANVAS"]);
const MEDIA_TAGS = new Set(["IMG", "IFRAME", "VIDEO"]);

/** How the words a line element stands in read it (HtmlContext.lines). */
export type LineWords = "lines" | "heading";

/** An element's name in capitals, an SVG drawing's too. */
export function tagOf(node: Node) {
  return node.nodeName.toUpperCase();
}

export function isLineElement(node: Node): node is Element {
  return node instanceof Element && LINE_TAGS.has(tagOf(node));
}

export function isMediaElement(node: Node): node is Element {
  return node instanceof Element && MEDIA_TAGS.has(tagOf(node));
}

/** Words a line element holds: a text area's text, a drawing's words; none for a script or a canvas. */
function lineText(element: Element) {
  const tag = tagOf(element);
  return tag === "TEXTAREA" || tag === "SVG" ? element.textContent ?? "" : "";
}

/** The place a heading's line element leaves, taking the spaces around it (paragraphText's heading words). */
export const HEADING_GAP = "\u0000";

/**
 * A line element or a frame or a video as the words of a <div>, a table cell
 * or a heading read it: a line of its own, kept when empty (the spaces mark
 * alone, blockLines), or in a heading its words without the spaces around.
 */
export function lineWords(element: Element, words: LineWords, escape: (text: string) => string) {
  const text = isLineElement(element) ? escape(lineText(element)).replace(/\s+/g, " ").trim() : "";
  if (words === "heading") return isLineElement(element) ? `${HEADING_GAP}${text}${HEADING_GAP}` : WORDS_GAP;
  if (isLineElement(element) && tagOf(element) === "TEXTAREA") return `\n${escape(lineText(element)).replace(/^\n|\n$/g, "") || WORDS_GAP}\n`;
  return `\n${text || WORDS_GAP}\n`;
}

/**
 * A paragraph's or a <div>'s nodes split at the line and media elements that
 * end them: each such element a part of its own, an element holding one split
 * too, its parts in copies of it that go on with the words around them
 * ("<p>앞 <span><img> 스팬</span> 뒤</p>" is "앞", the picture and
 * "스팬 뒤"). A paragraph ends at line elements wherever they stand and at
 * media right in it or in a <span>; a <div> at media alone.
 */
export function lineParts(nodes: Iterable<Node>, paragraph: boolean): Node[][] {
  const splits = (node: Node) => isMediaElement(node) || paragraph && isLineElement(node);
  const holds = (element: Element) => (paragraph && element.querySelector("script, textarea, svg, canvas") !== null)
    || tagOf(element) === "SPAN" && element.querySelector("img, iframe, video") !== null;
  const parts: Node[][] = [[]];
  for (const node of nodes) {
    if (splits(node)) parts.push([node], []);
    else if (node instanceof Element && holds(node)) {
      for (const part of lineParts(node.childNodes, paragraph)) {
        if (isElementPart(part)) parts.push(part, []);
        else if (part.length) {
          const shell = node.cloneNode(false) as Element;
          shell.append(...part.map((inner) => inner.cloneNode(true)));
          parts[parts.length - 1]!.push(shell);
        }
      }
    } else parts[parts.length - 1]!.push(node);
  }
  return parts.filter((part) => part.length);
}

/** Whether a part of lineParts is a line or media element of its own. */
export function isElementPart(part: Node[]) {
  return part.length === 1 && (isMediaElement(part[0]!) || isLineElement(part[0]!));
}
