import { CODE_SPACE } from "./htmlImportSpacing";

/**
 * Inline code of an imported HTML document, <code> or <tt>, as Notion's HTML
 * import reads it (seen 2026-11-13): code over the words it holds, which keep
 * their formatting — bold, italic, strikethrough, an underline, a link
 * ("<code>a <b>b</b></code>" is the code "a " and the bold code "b"); its
 * spaces collapsed as the document lays them out, a line break a space, the
 * spaces at its edges kept, those at a formatted part's edges moved out of
 * it, and one where two texts meet; a <br> in it ends a paragraph
 * (codeLineParts) and is nothing elsewhere.
 */

/** Where a piece of code's words opens and closes while a code's words are read. */
const OPEN = "\u000e";
const CLOSE = "\u000f";


/** Which elements inside a code format its words, and how they wrap the words they format. */
export type CodeFormatting = { formats: (element: Element) => boolean; wrap: (element: Element, words: string) => string };

/**
 * A code element's words as the importer's inline Markdown: each run of
 * words of one formatting a code span, the formatting around it.
 */
export function codeWords(code: Element, keep: boolean, skipped: (element: Element) => boolean, formatting: CodeFormatting) {
  const texts: Text[] = [];
  const walker = code.ownerDocument.createTreeWalker(code, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) texts.push(node as Text);
  const depth = (text: Text) => {
    let count = 0;
    for (let parent = text.parentElement; parent && parent !== code; parent = parent.parentElement) count += formatting.formats(parent) ? 1 : 0;
    return count;
  };
  const words = new Map(texts.map((text) => [text, keep ? text.data : text.data.replace(/[ \t\n\f\r]+/g, " ")]));
  for (let index = 1; !keep && index < texts.length; index += 1) {
    const [before, after] = [texts[index - 1]!, texts[index]!];
    if (!words.get(before)!.endsWith(" ") || !words.get(after)!.startsWith(" ")) continue;
    if (depth(after) > depth(before)) words.set(after, words.get(after)!.slice(1));
    else words.set(before, words.get(before)!.slice(0, -1));
  }
  const piece = (text: string | undefined) => (text ? `${OPEN}${text}${CLOSE}` : "");
  const read = (node: Node): string => {
    if (node.nodeType === Node.TEXT_NODE) return piece(words.get(node as Text));
    if (!(node instanceof Element) || skipped(node) || node.tagName === "BR") return "";
    let inner = Array.from(node.childNodes).map(read).join("").replace(EMPTY, "");
    if (!formatting.formats(node)) return inner;
    if (keep) return inner ? formatting.wrap(node, inner) : "";
    // Spaces at a formatted part's edges stand outside it.
    const lead = /^\u000e( +)/.exec(inner)?.[1] ?? "";
    inner = inner.replace(/^\u000e +/, OPEN).replace(EMPTY, "");
    const trail = /( +)\u000f$/.exec(inner)?.[1] ?? "";
    inner = inner.replace(/ +\u000f$/, CLOSE).replace(EMPTY, "");
    return `${piece(lead)}${inner ? formatting.wrap(node, inner) : ""}${piece(trail)}`;
  };
  const written = read(code).replace(new RegExp(`${CLOSE}${OPEN}`, "g"), "");
  return written.replace(new RegExp(`${OPEN}([^${CLOSE}]*)${CLOSE}`, "g"), (_, text: string) => {
    const spaced = keep ? text : text.replace(/ {2,}/g, " ");
    return spaced ? inlineCode(spaced).replace(/ /g, CODE_SPACE) : "";
  });
}

const EMPTY = new RegExp(`${OPEN}${CLOSE}`, "g");

/**
 * Inline code in a run of backticks longer than any in it, with a space more
 * at its edges where it has a backtick or a space at both (Markdown drops
 * one there).
 */
function inlineCode(code: string) {
  const fence = "`".repeat(Math.max(0, ...(code.match(/`+/g) ?? []).map((run) => run.length)) + 1);
  const space = /^`|`$/.test(code) || (/^ [\s\S]* $/.test(code) && code.trim()) ? " " : "";
  return `${fence}${space}${code}${space}${fence}`;
}

/**
 * A paragraph's code element holding a <br> split there into copies of it,
 * each <br> a part's end, as Notion's HTML import ends the paragraph
 * ("<p><code>a<br>b</code></p>" is the paragraphs "`a`" and "`b`").
 */
export function codeLineParts(node: Node): Node[] {
  if (!(node instanceof Element) || !["CODE", "TT"].includes(node.tagName) || !node.querySelector(":scope > br")) return [node];
  const parts: Node[] = [];
  let shell = node.cloneNode(false) as Element;
  for (const child of Array.from(node.childNodes)) {
    if (child instanceof Element && child.tagName === "BR") {
      parts.push(shell, child.cloneNode());
      shell = node.cloneNode(false) as Element;
    } else shell.append(child.cloneNode(true));
  }
  parts.push(shell);
  return parts;
}
