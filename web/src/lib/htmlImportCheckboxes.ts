import { collapseWords, KEPT_SPACES_MARK, keptLines } from "./htmlImportSpacing";

/**
 * A checkbox in an imported HTML document that is no export, as Notion's
 * HTML import reads one (seen 2026-11-13). In a list item (itemCheckbox):
 * the item's first checkbox outside the lists inside it, in a label or a
 * paragraph too, splits the item where it stands. The words before it are
 * the item's, and the item is a to-do, done as its box is, when there are
 * none; what follows the box is under the item — "<li><input> 글</li>" is an
 * empty to-do holding the paragraph "글", "<li>앞 <input> 뒤</li>" the item
 * "앞" holding "뒤"; a box in a formatted part of the item (bold, a link)
 * is left out with the spaces around it instead, the item a to-do when it
 * opens the item's words ("<li><b><input> 글</b> 뒤</li>" is the to-do
 * "**글** 뒤"). An export's to-do keeps its words
 * (importDocumentConverters.ts listItemBlocks).
 *
 * Outside a list, what a box makes is set by the block holding its words, a
 * paragraph standing for the block around it (boxMode): the page's body and a
 * quote split their words at each box (boxParts) — "<p>앞 <input> 뒤</p>" is
 * the paragraph "앞", an empty to-do and the paragraph "뒤", boxes with only
 * spaces between them one to-do, and a formatted part of the page's body
 * (bold, a link) opening with a box is an empty to-do holding its words —
 * "<b><input> 글</b> 뒤" is a to-do holding "**글**", then "뒤"; any other
 * block, a <div> or a <section>, makes a to-do of its words when they open
 * with a box (leadBox) — "<div><input> 글</div>" is the to-do "글", its
 * other boxes and line breaks as todoWords reads them — but a block right
 * in a quote is the quote's words; and a toggle's words
 * leave their boxes out. Where no box opens such words, and in a quote's or
 * a table cell's own words, a box is its mark on a line
 * of its own, "[ ] " or "[x] " (BoxWords); in a heading it is left out with
 * the spaces around it, and in a toggle's summary it is its mark in the line.
 */
export type ItemCheckbox = { checked: boolean; before: Node[]; after: Node[] };

/** How a checkbox reads in words (HtmlContext.box). */
export type BoxWords = "none" | "line" | "break" | "todo" | "inline" | "gap" | "tail";

/** What the words a box stands in become (boxMode). */
export type BoxMode = "split" | "todo" | "line" | "none";

/**
 * A box's part of words that a box splits: a box, a to-do of its words when
 * it has `words`; or the words between, under the box before them when
 * `under`, their boxes read in the line when `tail` (boxParts).
 */
export type BoxPart = { checked: boolean; words?: Node[]; spaced?: boolean } | (Node[] & { under?: boolean; tail?: boolean; code?: boolean });

/** The formatted parts of the page's body that hold the words after the box opening them (boxParts). */
const FORMATTING = new Set(["B", "STRONG", "I", "EM", "U", "INS", "S", "DEL", "STRIKE", "A"]);

/** The mark a box leaves in a to-do's words (BoxWords "todo"), which todoWords reads. */
const TODO_BOX = "\u0003";

const CHECKBOX = "input[type=checkbox i]";

/** The place a heading's box leaves, taking the spaces around it (boxWords "gap"). */
export const BOX_GAP = "\u0000";

export function isCheckbox(node: Node): node is HTMLInputElement {
  return node instanceof HTMLInputElement && node.matches(CHECKBOX);
}

function checkedBox(box: HTMLInputElement) {
  return box.checked || box.hasAttribute("checked");
}

/** Whether a node stands in a formatted part (bold, a link…) inside the element holding it. */
function formatted(node: Node, holder: Element) {
  for (let parent = node.parentElement; parent && parent !== holder; parent = parent.parentElement) {
    if (FORMATTING.has(parent.tagName)) return true;
  }
  return false;
}

export function itemCheckbox(item: Element): ItemCheckbox | null {
  // A box in a formatted part is the item's words (listItemBlocks), as Notion's HTML import reads it (seen 2026-11-13).
  const box = Array.from(item.querySelectorAll(CHECKBOX)).find((input) => input.closest("li") === item && !formatted(input, item));
  if (!(box instanceof HTMLInputElement)) return null;
  const checked = checkedBox(box);
  if (box.parentNode === item) {
    const parts = Array.from(item.childNodes);
    const at = parts.indexOf(box);
    return { checked, before: parts.slice(0, at), after: parts.slice(at + 1) };
  }
  // A box in a label or a paragraph: the parts on each side of it, in copies of what holds them.
  const range = item.ownerDocument.createRange();
  range.setStart(item, 0);
  range.setEndBefore(box);
  const before = Array.from(range.cloneContents().childNodes);
  range.setStartAfter(box);
  range.setEnd(item, item.childNodes.length);
  return { checked, before, after: Array.from(range.cloneContents().childNodes) };
}

/** Whether words hold a checkbox. */
export function holdsCheckbox(nodes: Iterable<Node>) {
  return Array.from(nodes).some((node) => isCheckbox(node) || node instanceof Element && node.querySelector(CHECKBOX) !== null);
}

/** What boxes make of the words a block holds, by the block — a paragraph by the block around it. */
export function boxMode(block: Element | null): BoxMode {
  let holder = block;
  while (holder?.tagName === "P") holder = holder.parentElement;
  if (!holder || holder.tagName === "BODY" || holder.tagName === "HTML" || holder.tagName === "BLOCKQUOTE") return "split";
  if (holder.tagName === "DETAILS") return "none";
  return holder.parentElement?.tagName === "BLOCKQUOTE" ? "line" : "todo";
}

/**
 * Words split at their boxes: the words between as they are, an element
 * holding a box split too, each of its parts in a copy of it standing apart
 * from the words around it (2026-11-13: "<p><span><input> 글</span> 뒤</p>"
 * is a to-do and the paragraphs "글" and "뒤"); boxes with only spaces
 * between them are the first of them. A formatted part holding a box where
 * nothing opens it — in the page's body, or in a paragraph after its words —
 * is a paragraph of its own, its boxes read in its line ("<b>앞 <input>
 * 뒤</b>" is "**앞[ ] ⏎뒤**", tailBoxes); one in the body opening with a
 * formatted part that a box opens is a to-do of its words, its own
 * formatting left out ("<b><i><input> 글</i> 뒤</b>" is the to-do "*글* 뒤"),
 * a <small> as such a part, a <span> as spanBoxParts reads it, and a <label>
 * or a <font> as words of the part (unwrapped); after a formatted part read
 * in its line, the body's formatted parts and code are each a block of their
 * own ("<b>앞 <input></b><i>기울</i> 글" is "**앞[ ] **", "*기울*" and "글");
 * one opening a paragraph is split at its box, its words after it under the
 * box — as Notion's HTML import reads them (seen 2026-11-13).
 */
export function boxParts(nodes: Iterable<Node>, where: "body" | "paragraph" | "other" = "other"): BoxPart[] {
  const parts: BoxPart[] = [];
  let words: Node[] = [];
  let said = false;
  let tail = false;
  const end = () => {
    if (words.length) parts.push(words);
    words = [];
  };
  for (const given of nodes) {
    const node = where === "body" ? unwrapped(given) : given;
    const formatting = where !== "other" && node instanceof Element && FORMATTING.has(node.tagName);
    const opening = formatting ? openingPart(node as Element) : undefined;
    const first = !said;
    said ||= node instanceof Element || Boolean((node.textContent ?? "").trim());
    const before = parts.length;
    if (isCheckbox(node)) {
      end();
      parts.push({ checked: checkedBox(node) });
    } else if (where === "body" && opening && isCheckbox(opening)) {
      end();
      parts.push({ checked: checkedBox(opening) }, ...heldWords(node as Element, opening));
    } else if (where === "body" && opening instanceof Element && OPENING_FORMATTING.has(opening.tagName) && leadBox([opening])) {
      end();
      parts.push({ checked: leadBox([opening])!.checked, words: Array.from(node.childNodes) });
    } else if (where === "body" && opening instanceof Element && opening.tagName === "SPAN" && isCheckbox(openingPart(opening) ?? opening)) {
      end();
      parts.push(...spanBoxParts(node as Element, opening));
    } else if (formatting && (node as Element).querySelector(CHECKBOX) && (where === "body" || !first)) {
      end();
      parts.push(Object.assign([node], { tail: true }));
      tail = where === "body";
      continue;
    } else if (tail && node instanceof Element && (FORMATTING.has(node.tagName) || isCode(node))) {
      // After a formatted part a box is in, its formatted parts and code stand apart, as Notion's HTML import has them.
      end();
      parts.push(Object.assign([node], isCode(node) ? { code: true } : {}));
      continue;
    } else if (formatting && first && opening && !isCheckbox(opening) && Array.from(node.childNodes).some(isCheckbox)) {
      end();
      const box = Array.from(node.childNodes).find(isCheckbox)!;
      const shell = node.cloneNode(false) as Element;
      shell.append(...Array.from(node.childNodes).slice(0, Array.from(node.childNodes).indexOf(box)).map((part) => part.cloneNode(true)));
      parts.push([shell], { checked: checkedBox(box) }, ...heldWords(node as Element, box));
    } else if (node instanceof Element && node.querySelector(CHECKBOX)) {
      end();
      for (const part of boxParts(node.childNodes)) {
        if (!Array.isArray(part)) parts.push(part);
        else {
          const shell = node.cloneNode(false) as Element;
          shell.append(...part.map((inner) => inner.cloneNode(true)));
          parts.push([shell]);
        }
      }
    } else words.push(node);
    if (parts.length > before) tail = false;
  }
  end();
  const blank = (part: BoxPart | undefined) => Array.isArray(part) && !part.some((node) => (node.textContent ?? "").trim());
  return parts.filter((part, index) => {
    if (Array.isArray(part)) return !(blank(part) && !Array.isArray(parts[index - 1]) && !Array.isArray(parts[index + 1]));
    let before = index - 1;
    while (before >= 0 && blank(parts[before])) before -= 1;
    return before < 0 || Array.isArray(parts[before]);
  });
}

/** The first part of an element's words: a box, a line break or the first node with words. */
function openingPart(element: Element) {
  return Array.from(element.childNodes).find((part) => isCheckbox(part) || (part.textContent ?? "").trim() || (part instanceof Element && part.tagName === "BR"));
}

/** The formatting a box opening a formatted part of the page's body may stand in, its part then a to-do of their words. */
const OPENING_FORMATTING = new Set([...FORMATTING, "SMALL"]);

function isCode(node: Node) {
  return node instanceof Element && (node.tagName === "CODE" || node.tagName === "TT");
}

/**
 * A formatted part of the page's body with the <label>s and <font>s right in
 * it holding a box left out, their words its own, as Notion's HTML import
 * reads them (seen 2026-11-13: "<b><font><input> 글</font> 뒤</b>" is the
 * to-do holding "**글 뒤**", as "<b><input> 글 뒤</b>" is); the node itself
 * for any other.
 */
function unwrapped(node: Node): Node {
  if (!(node instanceof Element) || !FORMATTING.has(node.tagName)) return node;
  const wrappers = Array.from(node.children).filter((child) => (child.tagName === "LABEL" || child.tagName === "FONT") && child.querySelector(CHECKBOX));
  if (!wrappers.length) return node;
  const copy = node.cloneNode(false) as Element;
  for (const child of Array.from(node.childNodes)) {
    if (wrappers.includes(child as Element)) copy.append(...Array.from(child.childNodes).map((part) => part.cloneNode(true)));
    else copy.append(child.cloneNode(true));
  }
  return copy;
}

/**
 * A formatted part of the page's body opening with a <span> a box opens, as
 * Notion's HTML import reads it (seen 2026-11-13): a to-do of the part's
 * words after the span, without the part's own formatting, the space opening
 * them kept ("<b><span><input> 글</span> 뒤</b>" is the to-do " 뒤"), holding
 * the span's words after the box, a paragraph up to each <br> or other box, in
 * the part's formatting ("**글**").
 */
function spanBoxParts(part: Element, span: Element): BoxPart[] {
  const box = Array.from(span.childNodes).find(isCheckbox)!;
  const after = Array.from(part.childNodes).slice(Array.from(part.childNodes).indexOf(span) + 1);
  const held = heldWords(span, box).map((words) => {
    const shell = part.cloneNode(false) as Element;
    shell.append(...(words as Node[]));
    return Object.assign([shell as Node], { under: true });
  });
  return [{ checked: checkedBox(box), words: after, spaced: /^\s/.test(after[0]?.textContent ?? "") }, ...held];
}

/**
 * The words a formatted part of the page's body holds after the box opening
 * it, under that box, as Notion's HTML import reads them (seen 2026-11-13):
 * a paragraph up to each <br> or other box, each in a copy of the part.
 */
function heldWords(part: Element, box: Node): BoxPart[] {
  const held: BoxPart[] = [];
  let words: Node[] = [];
  const end = () => {
    if (words.some((node) => (node.textContent ?? "").trim())) {
      const shell = part.cloneNode(false) as Element;
      shell.append(...words.map((node) => node.cloneNode(true)));
      held.push(Object.assign([shell as Node], { under: true }));
    }
    words = [];
  };
  for (const node of Array.from(part.childNodes).slice(Array.from(part.childNodes).indexOf(box as ChildNode) + 1)) {
    if (isCheckbox(node) || (node instanceof Element && node.tagName === "BR")) end();
    else words.push(node);
  }
  end();
  return held;
}

/** The box words open with, nothing but spaces before it; none when other words or a line break come first. */
export function leadBox(nodes: Iterable<Node>): { checked: boolean } | null {
  for (const node of nodes) {
    if (isCheckbox(node)) return { checked: checkedBox(node) };
    if (node.nodeType === Node.TEXT_NODE) {
      if ((node.textContent ?? "").trim()) return null;
    } else if (node instanceof Element) {
      if (node.tagName === "BR" || node.tagName === "IMG") return null;
      if (node.querySelector(CHECKBOX)) return leadBox(node.childNodes);
      if ((node.textContent ?? "").trim() || node.querySelector("br, img")) return null;
    }
  }
  return null;
}

/** The mark a box read in the line of a formatted part leaves (BoxWords "tail"), which tailBoxes reads. */
const TAIL_BOX = "\u0005";

/** The mark a <br> in such a part leaves, which tailBoxes reads (tailBreak). */
export const TAIL_BREAK = "\u0006";

/** The mark opening a formatted part inside such a part that holds a box, which tailBoxes reads (tailBreak). */
export const TAIL_BOX_HOLDER = "\u0007";

/** A checkbox as words read it (BoxWords), escaped for the importer's Markdown. */
export function boxWords(box: HTMLInputElement, words: BoxWords) {
  const mark = checkedBox(box) ? "\\[x\\]" : "\\[ \\]";
  switch (words) {
    case "tail": return `${TAIL_BOX}${checkedBox(box) ? "x" : "_"}${TAIL_BOX}`;
    case "line": return `\n${mark}&#32;\n`;
    case "break": return "\n";
    case "todo": return TODO_BOX;
    case "inline": return `${mark} `;
    case "gap": return BOX_GAP;
    default: return "";
  }
}

/**
 * A to-do's words, a block's that open with a box (leadBox), as Notion's
 * HTML import reads them (seen 2026-11-13): a <br> is a line break, those
 * side by side one; another box is one, but none right after a line break
 * or a box; a line break opening the words is kept before plain words
 * ("<div><input><br>글</div>" is a to-do of a blank line and "글", but
 * "<div><input><br><b>글</b></div>" one of "**글**") and one ending them is
 * not; their spaces as collapseWords keeps them.
 */
export function todoWords(written: string) {
  let text = "";
  let last: "start" | "words" | "break" | "box" = "start";
  for (const part of written.split(/(\n|\u0003)/)) {
    if (part === TODO_BOX) {
      if (last === "words") [text, last] = [`${text}\n`, "box"];
    } else if (part === "\n") {
      if (last !== "break") [text, last] = [`${text}\n`, "break"];
    } else if (part) {
      text += part;
      if (collapseWords(part)) last = "words";
    }
  }
  const lines = text.split("\n").map(collapseWords);
  while (lines.length && !lines[lines.length - 1]) lines.pop();
  // The opening line break is the words' only when plain words follow it, not a formatted part or a link.
  if (lines.length > 1 && !lines[0] && /^(?:\*|~~|`|\[|!\[)/.test(lines[1]!)) lines.shift();
  return keptLines(lines);
}

/**
 * Words whose boxes were read in their line (BoxWords "tail"), as Notion's
 * HTML import reads them (seen 2026-11-13): each box its mark and a line
 * break, the spaces around it left out ("앞 [ ] 뒤" is "앞[ ] ⏎뒤"), and those
 * before a formatted part holding it too ("앞 <i>기울 [ ]…" is "앞*기울[ ] ⏎…");
 * a box ending the words its mark and its space alone; and <br>s side by side
 * between words a line break more than there are of them ("[ ] <br>뒤" is
 * "[ ] ⏎⏎⏎뒤"), none ending the words.
 */
export function tailBoxes(words: string) {
  let ending = false;
  const written = words.replace(/[ \t]*\u0007/g, "").replace(/[ \t]*\u0005([x_])\u0005[ \t]*(?=([*~_]*$)?)/g, (_, checked: string, end?: string) => {
    const mark = checked === "x" ? "\\[x\\]&#32;" : "\\[ \\]&#32;";
    ending ||= end !== undefined;
    return end === undefined ? `${mark}&#10;` : mark;
  }).replace(/[ \t]*(\u0006+)[ \t]*(?=([*~_]*$)?)/g, (_, run: string, end?: string) => (end === undefined ? "&#10;".repeat(run.length + 1) : ""));
  // The space of a box ending the words is theirs, and a line break ending them, as the spaces mark keeps them.
  return ending || /&#10;[*~_]*$/.test(written) ? `${KEPT_SPACES_MARK}${written}` : written;
}

/**
 * Words with the place a box left (BOX_GAP) taken out with the spaces on each
 * side of it, past the formatting marks next to it.
 */
export function withoutBoxGaps(words: string) {
  if (!words.includes(BOX_GAP)) return words;
  return words.split(BOX_GAP).map((part, index, parts) => {
    const start = index > 0 ? part.replace(/^([*~_]*)[ \t]+/, "$1") : part;
    return index < parts.length - 1 ? start.replace(/[ \t]+([*~_]*)$/, "$1") : start;
  }).join("").trim();
}
