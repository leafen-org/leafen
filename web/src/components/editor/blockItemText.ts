import { i18next } from "@/i18n";

/**
 * A block item's words in the reader's language. A leaf the block catalogues
 * import, so modules they import read it whichever loads first
 * (scripts/check-module-cycles.mjs).
 */
export function blockItemText(key: string, values?: Record<string, unknown>) {
  return i18next.t(`blockItem:${key}`, values);
}
