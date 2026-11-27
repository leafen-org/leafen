import type { TextSpan } from "./types";

/**
 * A span's text color and its background, which Notion keeps apart (seen
 * 2026-10-10). An older span kept its background in its color; it reads as
 * that background.
 */
export function textSpanColors(span: Pick<TextSpan, "color" | "background">): { color?: string; background?: string } {
  const legacy = span.color?.endsWith("_background") ? span.color : undefined;
  const color = legacy ? undefined : span.color;
  const background = span.background || legacy;
  return { ...(color ? { color } : {}), ...(background ? { background } : {}) };
}
