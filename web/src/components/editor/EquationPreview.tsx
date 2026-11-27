"use client";

import { useMemo } from "react";
import katex from "katex";
import "katex/dist/katex.min.css";
import { HtmlDiv } from "@/lib/innerHtml";
import { assertRichtextHtmlTextBound, escapeHtml } from "./richtextHtmlEscaping";

// A formula KaTeX can't read shows KaTeX's own words for why, on a red
// box the width of the formula's, as Notion's math block (seen 2026-11-13:
// "KaTeX parse error: Undefined control sequence: \foo at position 1: …").
type RenderedEquation = { html: string; error?: undefined } | { error: string; html?: undefined };

function renderEquation(expression: string): RenderedEquation {
  assertRichtextHtmlTextBound(expression);
  // Spaces alone are a formula of nothing, drawn as one, as Notion's.
  const src = expression.trim() || (expression ? " " : "");
  if (!src) return { html: "" };
  try {
    return { html: katex.renderToString(src, { displayMode: true, throwOnError: true }) };
  } catch (error) {
    if (error instanceof katex.ParseError) return { error: error.message };
    return { html: escapeHtml(src) };
  }
}

export function EquationPreview({
  className,
  expression,
}: {
  className: string;
  expression: string;
}) {
  // KaTeX rendering is comparatively expensive; memoize on the source so
  // unrelated re-renders (parent state, selection changes) don't re-run it.
  const shown = useMemo(() => renderEquation(expression), [expression]);
  if (shown.error !== undefined) return <div className={className} data-equation-error="true">{shown.error}</div>;
  // KaTeX output is generated locally from the user's own LaTeX source
  // (no remote/untrusted HTML).
  return <HtmlDiv className={className} html={shown.html} />;
}
