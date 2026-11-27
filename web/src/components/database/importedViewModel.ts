import type { DbProperty, DbView } from "@/lib/types";

export type ImportedViewModelBounds = {
  maxProperties: number;
  maxPropertySettings: number;
};

function assertSafeBound(bound: number, label: string) {
  if (!Number.isSafeInteger(bound) || bound < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

function assertArrayBound(
  values: readonly unknown[],
  bound: number,
  label: string,
) {
  assertSafeBound(bound, label);
  if (values.length > bound) throw new RangeError(`${label} exceed their caller-owned bound.`);
}

function assertRecordEntryBound(
  value: Record<string, unknown>,
  bound: number,
  label: string,
) {
  assertSafeBound(bound, label);
  let count = 0;
  for (const key in value) {
    if (!Object.prototype.hasOwnProperty.call(value, key)) continue;
    count += 1;
    if (count > bound) throw new RangeError(`${label} exceed their caller-owned bound.`);
  }
}

function record(value: unknown) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function importedPropertySettingEntries(value: unknown, bounds: ImportedViewModelBounds) {
  if (Array.isArray(value)) {
    assertArrayBound(value, bounds.maxPropertySettings, "imported property settings");
    return value.map((setting) => ({ setting, fallback: undefined as string | undefined }));
  }
  const settings = record(value);
  if (!settings) return [];
  assertRecordEntryBound(settings, bounds.maxPropertySettings, "imported property settings");
  return Object.entries(settings).map(([fallback, setting]) => ({ setting, fallback }));
}

function settingPropertyReferences(setting: Record<string, unknown>, fallback?: string) {
  return [
    setting.propertyId,
    setting.property_id,
    setting.property,
    setting.property_ref,
    setting.propertyRef,
    setting.id,
    setting.name,
    setting.property_name,
    setting.propertyName,
    fallback,
  ].filter((value): value is string => typeof value === "string" && value.trim().length > 0);
}

/** Resolves the one imported List status whose source view presents it as task completion. */
export function importedListCompletionProperty(
  props: DbProperty[],
  view: DbView,
  bounds: ImportedViewModelBounds,
) {
  assertArrayBound(props, bounds.maxProperties, "database properties");
  if (view.type !== "list") return undefined;
  const native = props.find(prop => prop.type === "status" && prop.id === view.config?.statusCheckboxPropertyId);
  if (native) return native;
  const matches = new Map<string, DbProperty>();
  for (const { setting: rawSetting, fallback } of importedPropertySettingEntries(
    view.config?.notionPropertySettings,
    bounds,
  )) {
    const setting = record(rawSetting);
    if (!setting) continue;
    const showAs = setting.status_show_as ?? setting.statusShowAs;
    if (typeof showAs !== "string" || showAs.trim().toLowerCase() !== "checkbox") continue;
    const references = new Set(settingPropertyReferences(setting, fallback));
    for (const prop of props) {
      if (prop.type !== "status") continue;
      const notionPropertyId = prop.config?.notionPropertyId;
      if (
        references.has(prop.id)
        || references.has(prop.name)
        || (notionPropertyId && references.has(notionPropertyId))
      ) {
        matches.set(prop.id, prop);
      }
    }
  }
  return matches.size === 1 ? matches.values().next().value as DbProperty : undefined;
}
