import { i18next } from "@/i18n";

/** A product error text from the productErrors catalog, in the reader's language. */
export function productErrorText(key: string): string {
  return String(i18next.t(`productErrors:${key}`));
}
