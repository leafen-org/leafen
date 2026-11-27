import { useMemo, type ComponentPropsWithoutRef } from "react";

/**
 * Markup for `dangerouslySetInnerHTML` that keeps one object while the
 * markup is the same. React 19.2 set a new object's markup again on every
 * render, the same markup too, and the nodes under the pointer were drawn
 * anew: a click on a formula whose block the press had selected never came
 * (2026-11-01). React 19.3 compares the markup itself before setting it; the
 * one object keeps the markup in place whichever React draws it.
 */
export function useInnerHtml(html: string) {
  return useMemo(() => ({ __html: html }), [html]);
}

type HtmlProps<Tag extends "span" | "div"> = Omit<ComponentPropsWithoutRef<Tag>, "children" | "dangerouslySetInnerHTML"> & { html: string };

/** A block of markup, set again only when it changes (`useInnerHtml`). */
export function HtmlDiv({ html, ...props }: HtmlProps<"div">) {
  return <div {...props} dangerouslySetInnerHTML={useInnerHtml(html)} />;
}
