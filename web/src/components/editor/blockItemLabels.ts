import { i18next } from "@/i18n";
import { activeDateLocale, activeNumberLocale } from "@/lib/i18n";
import type { BlockType } from "@/lib/types";
import { BLOCK_DEFS } from "./blockDefinitions";
import { blockDefLabel, blockDefPlaceholder } from "./blockDefinitionLabels";
import { getDef } from "./blockDefinitionOrder";
import type { BlockTurnChoice } from "./blockTurnInto";

// Resolve at call time so locale changes also apply to non-hook helpers.
export { blockItemText } from "./blockItemText";

export function blockTypeLabel(type: BlockType) {
  return blockDefLabel(getDef(type));
}

function columnListTypeLabel(columnCount: number) {
  const definition = BLOCK_DEFS.find(
    (candidate) =>
      candidate.type === "column_list" && candidate.columnCount === columnCount
  );
  if (definition) return blockDefLabel(definition);

  // The palette intentionally offers only the common 2–5 quick starts, while
  // an existing layout can keep adding columns. Reuse the localized five-column
  // plural form as the grammatical template for higher visible counts so every
  // locale stays aligned without inventing an English-only fallback.
  if (columnCount > 5) {
    const pluralDefinition = BLOCK_DEFS.find(
      (candidate) => candidate.type === "column_list" && candidate.columnCount === 5
    );
    if (pluralDefinition) {
      const template = blockDefLabel(pluralDefinition);
      const formatter = new Intl.NumberFormat(activeNumberLocale());
      const localizedFive = formatter.format(5);
      if (template.includes(localizedFive)) {
        return template.replace(localizedFive, formatter.format(columnCount));
      }
      if (template.includes("5")) {
        return template.replace("5", String(columnCount));
      }
    }
  }

  return blockDefLabel(getDef("column_list"));
}

/** A "전환" choice's name: its type's, "페이지 위치:", its number of columns, or "Columns" for a column each. */
export function blockTurnChoiceLabel(choice: BlockTurnChoice) {
  if (choice.placeIn) return i18next.t("blockHandle:turnMenu.pageIn");
  if (choice.type !== "column_list") return blockTypeLabel(choice.type as BlockType);
  return choice.columns ? columnListTypeLabel(choice.columns) : i18next.t("blockHandle:turnMenu.columns");
}

export function blockTypePlaceholder(type: BlockType) {
  return blockDefPlaceholder(getDef(type));
}

export function blockItemLabels() {
  const t = i18next.t;
  return {
    addView: t("blockItem:addView"),
    cantCopyBlockHere: t("blockItem:cantCopyBlockHere"),
    cantMoveBlockHere: t("blockItem:cantMoveBlockHere"),
    copyViewLink: t("blockItem:copyViewLink"),
    dateDisplayLocale: activeDateLocale(),
    duplicateView: t("blockItem:duplicateView"),
    editIcon: t("blockItem:editIcon"),
    editLayout: t("blockItem:editLayout"),
    editTitle: t("blockItem:editTitle"),
    groupDate: t("blockItem:groupDate"),
    groupLinkToPage: t("blockItem:groupLinkToPage"),
    groupNewPage: t("blockItem:groupNewPage"),
    groupPeople: t("blockItem:groupPeople"),
    hideTitle: t("blockItem:hideTitle"),
    mentionToday: t("blockItem:mentionToday"),
    mentionTomorrow: t("blockItem:mentionTomorrow"),
    mentionYesterday: t("blockItem:mentionYesterday"),
    undo: t("blockItem:undo"),
    uploadFailed: (fileName: string) =>
      fileName
        ? t("blockItem:uploadFailed", { fileName })
        : t("blockItem:uploadFailedUnknown"),
    uploadingFile: (fileName: string) => t("blockItem:uploadingFile", { fileName }),
    viewDataSource: t("blockItem:viewDataSource"),
  };
}
