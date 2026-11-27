import { i18next } from "@/i18n";
import { blockDefKey, type BlockDef } from "./blockDefinitions";

/** i18next key for a block def's label. `action_delete` reuses the shared
 * common action label; a "Turn into" item is named for its block type, or
 * for its number of columns. */
export function blockLabelKey(def: BlockDef): string {
  if (def.action === "turn_into") {
    if (def.pageIn) return "blockHandle:turnMenu.pageIn";
    return `blocks:defs.${def.columnCount ? `column_list_${def.columnCount}` : blockDefKey({ type: def.type })}.label`;
  }
  const key = blockDefKey(def);
  return key === "action_delete" ? "common:actions.delete" : `blocks:defs.${key}.label`;
}

/** Display label for a block def in the active UI language. */
export function blockDefLabel(def: BlockDef): string {
  if (def.app) return def.app.title;
  return i18next.t(blockLabelKey(def));
}

/** Display description for a block def in the active UI language. */
export function blockDefDescription(def: BlockDef): string {
  if (def.appAction && def.app) return i18next.t("slashMenu:appActionDescription", { app: def.app.name });
  if (def.app) return i18next.t("slashMenu:appBlockDescription", { app: def.app.name });
  if (def.action === "turn_into") return i18next.t("blocks:defs.turn_into.description");
  return i18next.t(`blocks:defs.${blockDefKey(def)}.description`);
}

/** Display placeholder for a block def in the active UI language. */
export function blockDefPlaceholder(def: BlockDef): string {
  if (!def.placeholder) return "";
  return i18next.t(`blocks:defs.${blockDefKey(def)}.placeholder`);
}
