import { textWidth } from "@/lib/textWidth";
import type { DbProperty, Page } from "@/lib/types";
import { formatDateForProperty } from "./dateUtils";
import { formatNumberForProperty } from "./numberFormat";
import { valueAsRead } from "./valueAsRead";

/**
 * The room a timeline bar's shown values take before they are drawn and
 * measured (TimelineCanvas's fit): each run of values before or after the
 * title, its values 10px apart and 10px from the title, as measured. A value
 * counts by its words at the size it shows in (a chip's 13px, a value's
 * 12px) and a checkbox by its 14px box; one whose words this can't tell (a
 * formula, a person, a relation, a link shown shortened) counts nothing. So
 * it is never more than the values take once drawn, and a bar it finds too
 * short is narrow from its first frame (2026-11-15: counted as no values,
 * every one-day bar of a view showing six values was drawn as fitting and
 * turned narrow once measured, all 46 at once, ~12 ms at CPU ×2 inside the
 * view tab's press).
 */
export function timelineBarValuesWidth(row: Page, before: readonly DbProperty[], after: readonly DbProperty[]): number {
  return runWidth(row, before) + runWidth(row, after);
}

function runWidth(row: Page, run: readonly DbProperty[]) {
  let width = 0, shown = 0;
  for (const property of run) {
    const value = valueWidth(row, property);
    if (value === null) continue;
    width += value;
    shown += 1;
  }
  return shown ? width + 10 * shown : 0;
}

function valueWidth(row: Page, property: DbProperty): number | null {
  const value = valueAsRead(row, property);
  switch (property.type) {
    case "checkbox":
      return 14;
    case "select":
    case "multi_select":
    case "status": {
      const ids = new Set((Array.isArray(value) ? value : value ? [value] : []).map(String));
      const names = (property.config?.options ?? []).filter((option) => ids.has(option.id)).map((option) => option.name);
      // Its chips 6px apart (.timelineBarProp).
      return names.length ? names.reduce((sum, name) => sum + wordsWidth(name, "13px"), 6 * (names.length - 1)) : null;
    }
    case "number":
      return words(formatNumberForProperty(value, { config: (property.config ?? {}) as unknown as Record<string, unknown> }), "12px");
    case "date":
      return words(formatDateForProperty(value, property), "12px");
    case "rich_text":
      return words(value == null ? "" : String(value), "12px");
    default:
      return null;
  }
}

function words(text: string | null | undefined, size: string) {
  return text ? wordsWidth(text, size) : null;
}

// Words measured once a page's life (a view's values repeat across its bars
// and each time it is opened); forgotten as the page's fonts load, when they
// draw wider or narrower.
const measured = new Map<string, number>();
let fontsWatched = false;
function wordsWidth(text: string, size: string) {
  if (!fontsWatched && typeof document !== "undefined") {
    fontsWatched = true;
    document.fonts?.addEventListener("loadingdone", () => measured.clear());
  }
  const key = `${size} ${text}`;
  let width = measured.get(key);
  if (width === undefined) {
    if (measured.size > 4000) measured.clear();
    width = textWidth(text, size) ?? 0;
    measured.set(key, width);
  }
  return width;
}
