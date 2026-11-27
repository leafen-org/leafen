import katex from "katex";
import "katex/dist/katex.min.css";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { i18next } from "@/i18n";
import { InlineEquationIcon } from "@/icons/leafen/textMarkIcons";
import { escapeHtml } from "./richtextHtmlEscaping";

// Notion draws an inline equation in its line with KaTeX, as one piece the
// caret steps over (seen 2026-09-30). Its TeX stays the text the line keeps,
// in data-text, so edits around it count the TeX, not what is drawn.

/**
 * Why KaTeX can't read an equation's TeX, as its field says under it; null
 * when it can. A math block's is read as the block draws it, on a line of
 * its own, where \tag and the like belong (Notion's, seen 2026-11-13).
 */
export function inlineEquationError(tex: string, display = false): string | null {
  if (!tex.trim()) return null;
  try {
    katex.renderToString(tex, { displayMode: display, throwOnError: true });
    return null;
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
}

/**
 * An inline equation drawn from its TeX; TeX KaTeX can't read reads
 * "유효하지 않은 수학 공식입니다." in red in its line, as Notion's (seen
 * 2026-10-04).
 */
export function inlineEquationHtml(tex: string) {
  try {
    return katex.renderToString(tex, { displayMode: false, throwOnError: true });
  } catch {
    return `<span class="leafen-invalid-equation" style="color: var(--danger)">${escapeHtml(String(i18next.t("blockItem:equation.invalid")))}</span>`;
  }
}

/**
 * An inline equation with no expression, as page Markdown makes one: "√x
 * 새로운 수학 공식" in grey on a light chip, as Notion's (seen 2026-11-11);
 * a click edits it as any equation.
 */
export function emptyInlineEquationHtml() {
  const icon = renderToStaticMarkup(createElement(InlineEquationIcon, { "aria-hidden": "true" }));
  return `<span data-equation="" data-text="" data-equation-empty="true" contenteditable="false">${icon}${escapeHtml(String(i18next.t("blockItem:equation.new")))}</span>`;
}
