import type { TFunction } from "i18next";
import { i18next } from "@/i18n";

/**
 * Several names in one sentence, in Notion's list form (its formatSettings
 * messages, seen 2026-11-16 in the invite-and-mention panel's message field
 * and its invite snackbars, English and Korean): one name alone; two as
 * "a and b" — Korean "a, b"; three or more as the first and how many others,
 * "a, and 2 others" — Korean "a외 2개". Never every name joined by commas.
 */
export function listNames(names: readonly string[], t: TFunction = i18next.t): string {
  if (names.length < 2) return names[0] ?? "";
  if (names.length === 2) return t("common:listNames.two", { first: names[0], second: names[1] });
  return t("common:listNames.more", { first: names[0], others: names.length - 1 });
}
