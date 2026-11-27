import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";

type Translate = (key: string, options?: Record<string, unknown>) => unknown;
type PropertyNameLike = { name: string };

function translated(t: Translate, key: string, options?: Record<string, unknown>) {
  return String(t(key, options));
}

export function persistentGeneratedLabels(t: Translate) {
  return {
    untitled: translated(t, "common:labels.untitled"),
    importedDatabase: translated(t, "common:generated.importedDatabase"),
    uploadedFile: translated(t, "common:generated.uploadedFile"),
    image: translated(t, "common:generated.image"),
    propertyNames: {
      name: translated(t, "common:generated.propertyNames.name"),
      status: translated(t, "common:generated.propertyNames.status"),
      select: translated(t, "common:generated.propertyNames.select"),
      date: translated(t, "common:generated.propertyNames.date"),
    },
    statusOptions: {
      todo: translated(t, "common:generated.statusOptions.todo"),
      doing: translated(t, "common:generated.statusOptions.doing"),
      done: translated(t, "common:generated.statusOptions.done"),
    },
    selectOptions: {
      first: translated(t, "common:generated.selectOptions.first"),
      second: translated(t, "common:generated.selectOptions.second"),
    },
    viewNames: {
      table: translated(t, "common:generated.viewNames.table"),
    },
    columnName(number: number) {
      return translated(t, "common:generated.columnName", { number });
    },
    copyName(name: string) {
      return translated(t, "common:generated.copyName", { name });
    },
  };
}

export function activePersistentGeneratedLabels() {
  return persistentGeneratedLabels((key, options) => i18next.t(key, options));
}

/**
 * Labels persisted when pasted HTML holds a block without its content, or a
 * block pasted from another server is a line naming it (otherServerBlocks).
 */
export function activePastedBlockLabels() {
  const t = (key: string) => String(i18next.t(key));
  return {
    file: t("blocks:defs.file.label"),
    syncedBlock: t("blocks:defs.synced_block.label"),
    tabs: t("blocks:defs.tab.label"),
  };
}

/**
 * A collision-free name for a duplicated property, as Notion names one
 * ("메모 (1)", then "메모 (2)"; 2026-10-19: Leafen's read "숫자 사본").
 */
export function nextPropertyCopyName(
  properties: PropertyNameLike[],
  name: string,
  t: Translate,
) {
  const base = name.trim() || persistentGeneratedLabels(t).untitled;
  const names = new Set(properties.map((property) => property.name.trim().toLowerCase()));
  for (let number = 1; number < 1000; number += 1) {
    const candidate = `${base} (${number})`;
    if (!names.has(candidate.toLowerCase())) return candidate;
  }
  return `${base} (${newId().slice(0, 4)})`;
}
