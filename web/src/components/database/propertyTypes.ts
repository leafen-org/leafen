import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";
import { propertyOwnSettings } from "@/lib/propertyOwnSettings";
import type { PropertyConfig, PropertyType } from "@/lib/types";
import { createDefaultStatusConfig, statusConfigMadeFrom } from "@/lib/statusPropertyDefaults";

// Display order for the creatable property-type list. Labels are resolved
// lazily (at access time) so i18next has initialized before we read them.
const PROPERTY_TYPE_ORDER: PropertyType[] = [
  "rich_text",
  "number",
  "select",
  "multi_select",
  "status",
  "date",
  "person",
  "checkbox",
  "files",
  "relation",
  "rollup",
  "formula",
  "url",
  "email",
  "phone",
  "unique_id",
  "created_time",
  "last_edited_time",
  "created_by",
  "last_edited_by",
  "button",
  "location",
  "last_visited_time",
  "place",
  "verification",
];

const PROPERTY_TYPES: { type: PropertyType; label: string }[] = PROPERTY_TYPE_ORDER.map(
  (type) => ({
    type,
    get label() {
      return i18next.t(`propertyTypes:labels.${type}`);
    },
  })
);

// Any database can have a Verification property, which brings its Owner, as
// in Notion (decided 2026-10-30); a property isn't turned into one.
// Leafen's own location and last visited time stay for the columns that have
// them, but nothing fills them in, so none is made (decided 2026-10-16).
const NOT_MADE_TYPES = new Set<PropertyType>(["location", "last_visited_time"]);

// The order Notion's "유형 변경" lists types in (seen 2026-10-05), with
// Leafen's last visited time and location beside their kin where a property
// is one already.
const CHANGE_TYPE_ORDER: PropertyType[] = [
  "rich_text", "number", "select", "multi_select", "status", "date", "person", "files", "checkbox", "url", "email", "phone",
  "formula", "relation", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "last_visited_time",
  "button", "place", "location", "unique_id", "verification",
];

/**
 * The types a property can change to from the given one, as both property
 * menus list them, in Notion's order (seen 2026-10-05): every type, the
 * automatic ones too; "ID" shows dimmed, as Notion's does, and a
 * Verification only when it is the current type.
 */
export function editablePropertyTypes(currentType: PropertyType): { type: PropertyType; label: string; disabled?: boolean }[] {
  const byType = new Map(PROPERTY_TYPES.map((entry) => [entry.type, entry]));
  return CHANGE_TYPE_ORDER.flatMap((type) => {
    const entry = byType.get(type);
    if (!entry || ((type === "verification" || NOT_MADE_TYPES.has(type)) && currentType !== type)) return [];
    return [{ type, get label() { return entry.label; }, ...(type === "unique_id" && currentType !== type ? { disabled: true } : {}) }];
  });
}

export function propertyTypeLabel(type: PropertyType) {
  if (!PROPERTY_TYPE_ORDER.includes(type)) return type;
  return i18next.t(`propertyTypes:labels.${type}`);
}

/**
 * Whether a search for types finds the type: the letters typed, in order, in
 * the name the list shows ("텍트" finds 텍스트 and "트텍" nothing) or in its
 * English name — Notion's "유형 🔍" of a new property and of a property's
 * settings (seen 2026-11-17 in Chrome in Korean: "nmbr" finds 숫자, "fm"
 * 파일과 미디어 and 수식, "person" 사람, "edited by" 최종 편집자; "unique",
 * "people", "q" and "日付" nothing, so neither a type's key, other words nor
 * another language). The table's "+" reads the shown name alone
 * (`shownOnly`, seen 2026-11-15 and 2026-11-17: "number" and "date" find
 * nothing there, "d" only ID).
 */
export function propertyTypeFound(type: PropertyType, query: string, { shownOnly = false } = {}) {
  const letters = query.replace(/\s+/gu, "").toLocaleLowerCase();
  if (lettersInOrder(propertyTypeLabel(type), letters)) return true;
  if (shownOnly || !PROPERTY_TYPE_ORDER.includes(type)) return false;
  // A screen in Korean holds these English names once its start has settled (englishAlsoRead).
  const english = i18next.getResource("en", "propertyTypes", `labels.${type}`);
  return typeof english === "string" && lettersInOrder(english, letters);
}

function lettersInOrder(name: string, letters: string) {
  const lower = name.toLocaleLowerCase();
  let at = 0;
  for (const letter of letters) {
    at = lower.indexOf(letter, at);
    if (at < 0) return false;
    at += letter.length;
  }
  return true;
}

export function localizedPropertyTypeLabel(type: PropertyType) {
  return i18next.t(`propertyTypes:labels.${type}`);
}

export function configForType(
  type: PropertyType,
  config: PropertyConfig | undefined,
  databaseId: string,
  /** The type a property changes from, whose settings `config` is. */
  fromType?: PropertyType,
): PropertyConfig | undefined {
  // What a property is whatever its type goes along: how a page shows it,
  // its description and its icon, as Notion's keep (seen 2026-11-06: a
  // number's ⓘ through text and back), and its place in its database's page
  // order (propertyOwnSettings).
  const displayConfig = propertyOwnSettings(config);
  const withDisplayConfig = (next?: PropertyConfig): PropertyConfig | undefined => {
    const merged = { ...(next ?? {}), ...displayConfig } as PropertyConfig;
    return Object.keys(merged).length ? merged : undefined;
  };
  if (type === "status") {
    if (!config) {
      return withDisplayConfig(createDefaultStatusConfig(newId, i18next.language));
    }
    if (config.statusGroups) return withDisplayConfig({ options: config.options ?? [], statusGroups: config.statusGroups });
    // Made from another type, it has Notion's groups and options (statusConfigMadeFrom).
    return withDisplayConfig(statusConfigMadeFrom(fromType, config.options ?? [], newId, i18next.language));
  }
  if (type === "select" || type === "multi_select") {
    return withDisplayConfig({ options: config?.options ?? [] });
  }
  // A number has a format only once one is picked, as Notion's (seen
  // 2026-11-14: its MCP names none for one the "+" menu added), and shows
  // as a plain number until then (numberFormatForProperty).
  if (type === "number") return withDisplayConfig(config?.numberFormat ? { numberFormat: config.numberFormat } : undefined);
  if (type === "relation") {
    return withDisplayConfig({
      relationDatabaseId: config?.relationDatabaseId ?? databaseId,
      // Preserve the two-way pair link across config rebuilds (e.g. turn-into).
      ...(config?.relatedPropertyId ? { relatedPropertyId: config.relatedPropertyId } : {}),
    });
  }
  if (type === "rollup") {
    return withDisplayConfig({
      rollupRelationPropertyId: config?.rollupRelationPropertyId,
      rollupTargetPropertyId: config?.rollupTargetPropertyId,
      rollupFunction: config?.rollupFunction ?? "show_original",
    });
  }
  if (type === "formula") return withDisplayConfig({ formula: config?.formula ?? "" });
  return withDisplayConfig(undefined);
}
