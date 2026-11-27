/**
 * Stable retained UI-model entry point. The generated Go↔React wire modules remain the transport authority.
 */
import type { TextSpan } from "./modelTypes/content";

export * from "./modelTypes/core";
export * from "./modelTypes/page";
export * from "./modelTypes/content";
export * from "./modelTypes/database";
export * from "./modelTypes/views";
export * from "./modelTypes/admin";
export * from "./modelTypes/organization";

// Convenience: a text-bearing block's plain text from its rich spans.
export function spansToPlainText(spans?: TextSpan[]): string {
  return (spans ?? []).map((s) => s.text).join("");
}
