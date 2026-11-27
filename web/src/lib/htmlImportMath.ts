import { escapeImportMarkdown } from "./importMarkdownText";
import { tagOf } from "./htmlImportLineElements";

/**
 * MathML (<math>) in an imported HTML document that is no export, as Notion's
 * HTML import reads it (seen 2026-11-13): words, never an equation. A <math>
 * holding a fraction, a root, a table, a phantom or limits under or over
 * (COMPLEX) is its LaTeX between dollar signs as words ("$\frac{a}{b}$",
 * mathLatex); any other is its symbols drawn as text (mathRuns): an
 * identifier of one character in italics, of more in upright letters and a
 * thin space after it ("sin "), one whose mathvariant (its own or its
 * <mstyle>'s) is bold in bold; numbers and text as written; an operator
 * between operands with spaces of its kind on each side — a binary one a
 * medium space ("x + 1"), a relation a thick one ("α = β"), a comma a thin one
 * after it — its lspace and rspace not read, "-" the minus sign; scripts after
 * their base ("xi2"), an accent over its base combined with it ("x̂"), a
 * string between quotation marks, a named space or one of 1em its width, and
 * annotations and invisible operators nothing. A KaTeX drawing's own HTML
 * (.katex-html, isMathDrawing) is left out, its MathML read. Maths side by
 * side are one (2026-11-13: "<math>x</math><math>y</math>" is "_xy_").
 */

const COMPLEX = new Set(["MFRAC", "MSQRT", "MROOT", "MTABLE", "MPHANTOM", "MUNDEROVER", "MUNDER"]);
const UNREAD = new Set(["ANNOTATION", "ANNOTATION-XML"]);
const BINARY = new Set(["+", "-", "−", "±", "∓", "×", "⋅", "·", "÷", "∗", "∘", "∪", "∩", "⊕", "⊗", "∧", "∨"]);
const RELATION = new Set(["=", "<", ">", "≤", "≥", "≠", "≈", "≡", "∼", "≅", "∝", "→", "←", "↔", "⇒", "⇐", "⇔", "↦", "∈", "∉", "⊂", "⊃", "⊆", "⊇", "≺", "≻"]);
const SEPARATOR = new Set([",", ";"]);
/** Closing marks after which an operator is between operands. */
const CLOSING = new Set([")", "]", "}", "|", "!", "′", "″"]);
const ACCENTS = new Map([["^", "̂"], ["ˆ", "̂"], ["~", "̃"], ["˜", "̃"], ["¯", "̄"], ["‾", "̄"], ["˙", "̇"], ["¨", "̈"], ["→", "⃗"]]);
const LARGE = new Map([["∑", "\\sum"], ["∏", "\\prod"], ["∫", "\\int"], ["⋃", "\\bigcup"], ["⋂", "\\bigcap"]]);
const NAMED_SPACES = new Map([["thinmathspace", " "], ["mediummathspace", " "], ["thickmathspace", " "], ["1em", " "]]);

const THIN = " ";
const MEDIUM = " ";
const THICK = " ";

type Run = { text: string; italic: boolean; bold: boolean };

/** Whether an element is a KaTeX drawing's own HTML, which Notion's import leaves out beside its MathML. */
export function isMathDrawing(element: Element) {
  return element.classList.contains("katex-html");
}

export function isMath(node: Node | null): node is Element {
  return node instanceof Element && tagOf(node) === "MATH";
}

/**
 * A <math>'s words as the importer's inline Markdown, with the maths right
 * after it; none for one right after another, which that one read. Its
 * italics and bold are left out inside words already in them.
 */
export function mathWords(math: Element) {
  if (isMath(math.previousSibling)) return "";
  const maths = [math];
  for (let next = math.nextSibling; isMath(next); next = next.nextSibling) maths.push(next);
  if (maths.some(holdsComplex)) return escapeImportMarkdown(`$${maths.map((part) => latex(part)).join("")}$`);
  const runs: Run[] = [];
  for (const part of maths) drawn(part, runs);
  const italic = math.closest("i, em") !== null;
  const bold = math.closest("b, strong") !== null;
  return joined(runs).map((run) => {
    const text = escapeImportMarkdown(run.text).replace(/[ - ]/g, (space) => `&#${space.charCodeAt(0)};`);
    const marks = `${run.bold && !bold ? "**" : ""}${run.italic && !italic ? "*" : ""}`;
    return marks ? `${marks}${text}${[...marks].reverse().join("")}` : text;
  }).join("");
}

function holdsComplex(element: Element): boolean {
  return Array.from(element.children).some((child) => {
    const tag = tagOf(child);
    if (UNREAD.has(tag)) return false;
    if (COMPLEX.has(tag) || tag === "MOVER" && !accentOf(child)) return true;
    return holdsComplex(child);
  });
}

/** The combining mark an <mover>'s accent draws over its base; none when it is no accent. */
function accentOf(over: Element) {
  const mark = over.children[1];
  return mark && tagOf(mark) === "MO" ? ACCENTS.get(words(mark)) : undefined;
}

function words(element: Element) {
  return (element.textContent ?? "").replace(/\s+/g, " ").trim();
}

/** The operator an <mo> draws: the minus sign for "-", nothing for an invisible one. */
function operator(element: Element) {
  const symbol = words(element);
  return /^[⁡-⁤]$/.test(symbol) ? "" : symbol === "-" ? "−" : symbol;
}

/** Whether an operator stands between operands: something before it in its row that is no operator but a closing one. */
function between(element: Element) {
  const before = element.previousElementSibling;
  return before !== null && (tagOf(before) !== "MO" || CLOSING.has(operator(before)));
}

function variant(element: Element) {
  for (let at: Element | null = element; at && tagOf(at) !== "MATH"; at = at.parentElement) {
    const value = at.getAttribute("mathvariant");
    if (value) return value.toLowerCase();
  }
  return "";
}

function drawn(element: Element, runs: Run[]) {
  const tag = tagOf(element);
  const add = (text: string, italic = false, bold = false) => text && runs.push({ text, italic, bold });
  switch (tag) {
    case "MI": {
      const text = words(element);
      const kind = variant(element);
      const bold = kind.startsWith("bold");
      const italic = !bold && (kind === "italic" || [...text].length === 1);
      add(text, italic, bold);
      if (!bold && !italic) add(THIN);
      return;
    }
    case "MN": case "MTEXT":
      add(words(element));
      return;
    case "MS":
      add(`“${words(element)}”`);
      return;
    case "MSPACE":
      add(NAMED_SPACES.get((element.getAttribute("width") ?? "").trim().toLowerCase()) ?? "");
      return;
    case "MO": {
      const symbol = operator(element);
      if (SEPARATOR.has(symbol)) add(`${symbol}${THIN}`);
      else if ((BINARY.has(symbol) || RELATION.has(symbol)) && between(element)) {
        const space = RELATION.has(symbol) ? THICK : MEDIUM;
        add(`${space}${symbol}${space}`);
      } else add(symbol);
      return;
    }
    case "MOVER": {
      const accent = accentOf(element);
      if (element.children[0]) drawn(element.children[0], runs);
      if (accent && runs.length) runs[runs.length - 1]!.text += accent;
      return;
    }
    case "SEMANTICS":
      if (element.children[0]) drawn(element.children[0], runs);
      return;
    default:
      if (UNREAD.has(tag)) return;
      for (const child of Array.from(element.children)) drawn(child, runs);
  }
}

/** Runs side by side in one style as one. */
function joined(runs: Run[]) {
  const out: Run[] = [];
  for (const run of runs) {
    const last = out[out.length - 1];
    if (last && last.italic === run.italic && last.bold === run.bold) last.text += run.text;
    else out.push({ ...run });
  }
  return out;
}

/** An element's LaTeX, as Notion's import writes a <math> it draws no text for. */
function latex(element: Element): string {
  const tag = tagOf(element);
  const parts = Array.from(element.children);
  const at = (index: number) => (parts[index] ? latex(parts[index]!) : "");
  switch (tag) {
    case "MI": case "MN": case "MTEXT":
      return words(element);
    case "MS":
      return `“${words(element)}”`;
    case "MO": {
      const symbol = operator(element);
      if ((BINARY.has(symbol) || RELATION.has(symbol)) && between(element)) return ` ${symbol} `;
      return LARGE.get(symbol) ?? symbol;
    }
    case "MFRAC": return `\\frac{${at(0)}}{${at(1)}}`;
    case "MSQRT": return `\\sqrt{${parts.map(latex).join("")}}`;
    case "MROOT": return `\\sqrt[${at(1)}]{${at(0)}}`;
    case "MSUB": return `${at(0)}_{${at(1)}}`;
    case "MSUP": return `${at(0)}^{${at(1)}}`;
    case "MSUBSUP": return `${at(0)}_{${at(1)}}^{${at(2)}}`;
    case "MUNDER": return `${at(0)}\\limits_{${at(1)}}`;
    case "MOVER": return accentOf(element) ? at(0) : `${at(0)}\\limits^{${at(1)}}`;
    case "MUNDEROVER": return `${at(0)}\\limits_{${at(1)}}^{${at(2)}}`;
    case "MPHANTOM": return `\\phantom{${parts.map(latex).join("")}}`;
    case "MTABLE":
      return `\\begin{matrix}\n${parts.map((row) => Array.from(row.children).map(latex).join(" & ")).join(" \\\\\n")}\n\\end{matrix}`;
    case "MSPACE": return "";
    case "SEMANTICS": return at(0);
    default:
      return UNREAD.has(tag) ? "" : parts.map(latex).join("");
  }
}

/**
 * An equation as Notion's HTML export writes it, as Notion's HTML import
 * reads it back (seen 2026-11-15): a figure.equation holding its TeX in
 * data-notion-equation a math block, a span holding it in
 * data-notion-inline-equation an inline equation, their KaTeX drawings left
 * out. Leafen's export writes them so too. As the importer's Markdown
 * (workspace_import_markdown_math.go): a block "$$TeX$$" on its lines
 * without a blank one, an inline one "$TeX$" on one line without spaces at
 * its ends; null for any other element.
 */
export function notionEquationMarkdown(element: Element): string | null {
  const block = tagOf(element) === "FIGURE" ? element.getAttribute("data-notion-equation") : null;
  if (block !== null) return block.trim() ? `$$${block.trim().replace(/\n\s*\n/g, "\n")}$$` : "";
  const inline = element.getAttribute("data-notion-inline-equation");
  if (inline === null) return null;
  return inline.trim() ? `$${inline.trim().replace(/\s*\n\s*/g, " ")}$` : "";
}
