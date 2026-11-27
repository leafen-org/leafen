import { i18next } from "@/i18n";
import { personName } from "@/lib/peopleDirectory";
import type { DbProperty } from "@/lib/types";
import { databaseViewLabels } from "./databaseViewLabels";
import { statusGroupColumns } from "./propertyStatusPolicy";
import { relativeSprintOf } from "@/shared/database/queryFilterPolicy";
import { isCurrentPageFilterValue, isCurrentUserFilterValue } from "./query";
import { isRelativeFilterDay } from "@/shared/database/queryRelativeDate.mjs";
import { relativeFilterDayText } from "./DateFilterDayChoice";

const PEOPLE_TYPES = new Set(["person", "created_by", "last_edited_by"]);
const DATE_TYPES = new Set(["date", "created_time", "last_edited_time"]);

/**
 * A filter's value in words: its options' names, a checkbox's
 * state, people by name ("나" for whoever reads it), pages by the name the
 * caller finds for them — "this page" by the name its view gives it
 * (FilterThisPage: a template's own), else "이 페이지" — a sprint relation's
 * "현재", "다음" or "마지막".
 */
export function filterValueText(value: unknown, property: DbProperty, pageName?: (id: string) => string, thisPageName?: string): string {
  const labels = databaseViewLabels();
  const options = Array.isArray(property.config?.options)
    ? property.config.options as Array<{ id?: string; name?: string }>
    : [];
  // A status may be filtered by a whole group, named as it is written ("Stage:
  // Done,To-do"), not in the reader's language, as Notion's pill (seen 2026-11-11).
  const groups = property.type === "status" ? statusGroupColumns(property.config) : [];
  const one = (item: unknown) => {
    if (PEOPLE_TYPES.has(property.type) && isCurrentUserFilterValue(item)) return labels.me;
    if (PEOPLE_TYPES.has(property.type) && typeof item === "string") return personName(item) || item;
    if (property.type === "relation" && relativeSprintOf(item)) return i18next.t(`databaseView:relativeSprints.${relativeSprintOf(item)}`);
    if (property.type === "relation" && isCurrentPageFilterValue(item)) return thisPageName || labels.layoutTabThisPage;
    // A day named from today in its words ("1주 전").
    if (DATE_TYPES.has(property.type) && isRelativeFilterDay(item)) return relativeFilterDayText(item);
    if (property.type === "relation" && typeof item === "string" && pageName) return pageName(item) || item;
    if (typeof item === "string") {
      const option = options.find(({ id, name }) => id === item || name === item);
      if (option) return option.name ?? item;
      return groups.find(({ id, name }) => id === item || name === item)?.name ?? item;
    }
    if (typeof item === "boolean") return property.type === "checkbox" ? (item ? labels.checked : labels.unchecked) : String(item);
    if (typeof item === "number") return String(item);
    return "";
  };
  // Notion joins a select's chosen options with bare commas ("Early,Late").
  if (Array.isArray(value)) return value.map(one).filter(Boolean).join(",");
  return one(value);
}
