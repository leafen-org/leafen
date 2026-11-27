/**
 * How an imported HTML document lays out its text's spaces and line breaks:
 * by each element's white-space as the document's own style sets it, as a
 * browser shows the document. Spaces and line breaks collapse ("collapse"),
 * line breaks alone are kept ("lines", pre-line), or both are kept ("keep",
 * pre and pre-wrap) — as Notion's HTML export keeps its text's own, its body
 * being pre-wrap (2026-11-13: a caption's "…끝 " came back "…끝", where
 * Notion's API import keeps it). A document that is no export without such
 * a style reads the line breaks in its text as lines (brokenText).
 */
export type HtmlSpacing = "collapse" | "lines" | "keep";

const KEEPING_TAGS = new Set(["PRE", "TEXTAREA", "LISTING", "PLAINTEXT", "XMP"]);

/** What a declaration's white-space keeps; none when it sets none or inherits it. */
function declaredSpacing(style: CSSStyleDeclaration | undefined): HtmlSpacing | undefined {
  const value = (style?.getPropertyValue("white-space") || style?.getPropertyValue("white-space-collapse") || "").trim().toLowerCase();
  if (!value || value === "inherit" || value === "unset") return undefined;
  if (/^(?:pre-line|preserve-breaks)\b/.test(value)) return "lines";
  if (/^(?:pre|pre-wrap|break-spaces|preserve|preserve-spaces)\b/.test(value)) return "keep";
  return "collapse";
}

/** The document's style rules that set white-space, in their order. */
function spacingRules(document: Document) {
  const rules: { selector: string; spacing: HtmlSpacing }[] = [];
  for (const style of Array.from(document.querySelectorAll("style"))) {
    try {
      let sheet = style.sheet;
      if (!sheet) {
        sheet = new CSSStyleSheet();
        sheet.replaceSync(style.textContent ?? "");
      }
      for (const rule of Array.from(sheet.cssRules)) {
        if (!("selectorText" in rule) || !("style" in rule)) continue;
        const spacing = declaredSpacing((rule as CSSStyleRule).style);
        if (spacing) rules.push({ selector: (rule as CSSStyleRule).selectorText, spacing });
      }
    } catch {
      // A style this engine cannot read sets nothing.
    }
  }
  return rules;
}

/**
 * The spacing of each element of a document: its own style's, else the last
 * rule's that matches it, else a preformatted element's, else its parent's.
 */
export function documentSpacing(document: Document) {
  const rules = spacingRules(document);
  const known = new Map<Element, HtmlSpacing>();
  const spacing = (element: Element | null): HtmlSpacing => {
    if (!element) return "collapse";
    const cached = known.get(element);
    if (cached) return cached;
    let own = declaredSpacing((element as HTMLElement).style);
    for (let index = rules.length - 1; own === undefined && index >= 0; index -= 1) {
      try {
        if (element.matches(rules[index]!.selector)) own = rules[index]!.spacing;
      } catch {
        // A selector this engine cannot match matches nothing.
      }
    }
    const result = own ?? (KEEPING_TAGS.has(element.tagName) ? "keep" : spacing(element.parentElement));
    known.set(element, result);
    return result;
  };
  return spacing;
}

/**
 * Where a document that keeps none of its spaces has words of two texts side
 * by side, their spaces are not one, as Notion's HTML import reads them
 * (seen 2026-11-13): the spaces on each side of an element that leaves no
 * words — a form field, a comment, an empty element, one a list item's words
 * move out — are both kept (GAP), and those on each side of an element that
 * is no formatting, a <span>, a <font>, a <label> (EDGE), but in a heading,
 * where only a GAP keeps them; spaces at a formatted part's edges (bold, a
 * link) and in one text are one ("a <input> b" is "a  b", "a <b> b</b> c"
 * is "a **b** c").
 */
export const WORDS_GAP = "\u0001";
export const WORDS_EDGE = "\u0002";

/**
 * A space inside inline code, which the spaces of the words around it do not
 * collapse with (htmlImportInlineCode.ts); collapseWords and withoutWordMarks
 * write it back.
 */
export const CODE_SPACE = "\u0004";

/** A line's spaces as WORDS_GAP and WORDS_EDGE keep them, its edges trimmed and the marks left out. */
export function collapseWords(line: string) {
  return line.replace(/[\s\u0001\u0002]+/g, (run) => run.split(/[\u0001\u0002]/).map((part) => (part ? " " : "")).join("")).trim()
    .replace(/\u0004/g, " ");
}

/** Words without the WORDS_GAP and WORDS_EDGE marks. */
export function withoutWordMarks(words: string) {
  return words.replace(/[\u0001\u0002]/g, "").replace(/\u0004/g, " ");
}

/** A text's spaces and line breaks as its element lays them out. */
export function spacedText(text: string, spacing: HtmlSpacing) {
  if (spacing === "keep") return text;
  if (spacing === "lines") return text.replace(/[ \t\f\r]*\n[ \t\f\r]*/g, "\n").replace(/[ \t\f\r]+/g, " ");
  return text.replace(/[ \t\n\f\r]+/g, " ");
}

/**
 * A text's spaces and line breaks in a document that is no export, where its
 * style keeps none, as Notion's HTML import reads them (seen 2026-11-13: an
 * indented document's "가\n    <b>굵게</b>\n    나" is three lines): a run
 * of spaces holding a line break is a line break, any other run a space;
 * none right next to a <br>, which is a line break of its own, and spaces
 * holding a line break alone between two formatted parts are none.
 */
export function brokenText(text: Text, formatted: (node: Node | null) => boolean) {
  const words = text.data;
  if (!/\S/.test(words) && words.includes("\n") && formatted(text.previousSibling) && formatted(text.nextSibling)) return "";
  const broken = (node: Node | null) => node instanceof Element && node.tagName === "BR";
  let lines = words;
  if (broken(text.previousSibling)) lines = lines.replace(/^[ \t\f\r]*\n[ \t\n\f\r]*/, "");
  if (broken(text.nextSibling)) lines = lines.replace(/[ \t\n\f\r]*\n[ \t\f\r]*$/, "");
  return lines.replace(/[ \t\n\f\r]+/g, (run) => (run.includes("\n") ? "\n" : " "));
}

/**
 * A kept line with the spaces at its edges written as character references,
 * which the Markdown the importer reads keeps where it drops a line's edge
 * spaces.
 */
export function keptEdges(line: string) {
  return line.replace(/^[ \t]|[ \t]$/g, (space) => `&#${space.charCodeAt(0)};`);
}

/**
 * The mark that opens a block's words whose spaces and line breaks at their
 * ends are words: the importer drops a block's edge spaces as Notion's
 * Markdown import does (workspace_import_markdown_marks.go), where a document
 * whose style keeps its spaces has them as its words, as Notion's API import
 * keeps those of Notion's HTML export.
 */
export const KEPT_SPACES_MARK = "[](leafen-spaces:)";

/**
 * A kept block's lines: each line's edge spaces kept, a line break next to
 * an empty line as a character reference (Markdown drops a block's blank
 * lines and the line breaks at its ends: "<p>a<br><br>b</p>" of Notion's
 * export is "a", a blank line and "b", as Notion's API import has it), and
 * its words marked when they have spaces at their ends. None when it holds
 * only spaces.
 */
export function keptLines(lines: string[]) {
  if (!lines.some((line) => line.trim())) return "";
  const text = lines.map((line, index) => (index === 0 ? "" : line && lines[index - 1] ? "\n" : "&#10;") + keptEdges(line)).join("");
  return /^\s|\s$/.test(lines.join("\n")) ? `${KEPT_SPACES_MARK}${text}` : text;
}
