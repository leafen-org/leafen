import { i18next } from "@/i18n";
import type { GraphBlock } from "@/contentGraph";
import { rememberCodeBlockLanguage } from "@/lib/blockDefaults";
import { requestInlineDatabase } from "@/lib/inlineDatabaseRequests";
import { blockTypeLabel } from "./blockActionMenuItems";
import { setIconCompanions, takeIconCompanions } from "./blockIconCompanions";
import { blockItemText } from "./blockItemLabels";
import { blockMenuShape, type BlockMenuShape, type SeveralBlockKind } from "./blockMenuShape";
import type { BlockListFormat } from "./BlockMenuSubmenu";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { requestCalloutIconEdit } from "./CalloutIcon";
import { codeLanguageOptions } from "./codeLanguages";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { inlineDatabaseShownView } from "./useBlockDatabaseMenu";
import { childPageBlockPageId } from "./useBlockPageMenu";

/**
 * What the block menu of several chosen blocks shows beyond its common rows
 * (blockActionMenuItems): its head, the rows every one of them takes, and
 * what its first in the page leads.
 */
export type BlockMenuSeveral = {
  /** "전환" and "색" where every one of them has it (blockMenuShape). */
  shape: BlockMenuShape;
  /** The first one's own menu head where all are of its kind, else "블록 N개", with the first inline database's view. */
  head: string;
  /** "휴지통으로 이동" for the delete: the first is a sub-page. */
  deleteToTrash: boolean;
  /** "복제 ›" with and without content: the first is a database. */
  duplicateKinds: boolean;
  /** "아이콘 편집" for them all: two or more sub-pages, full-page databases' lines or callouts. */
  onEditIcon?: () => void;
  /** "언어 ›" for them all: code blocks. */
  language?: BlockListFormat;
};

/** A chosen block's kind: a full-page database's line is apart from a sub-page's, as Notion's menus tell them apart. */
function severalKind(adapter: NativeEditorGraphAdapter, block: GraphBlock): SeveralBlockKind {
  if (block.type === "child_database") return "database_page";
  if (block.type !== "child_page") return block.type;
  const pageId = childPageBlockPageId(block);
  return adapter.moveContext?.pages.find((page) => page.id === pageId)?.kind === "database" ? "database_page" : "child_page";
}

/** The head of one block's own menu (BlockActionMenu, useBlockPageMenu, useBlockDatabaseMenu). */
function ownHead(kind: SeveralBlockKind, view: string | undefined) {
  if (kind === "child_page") return i18next.t("blockHandle:page.header");
  if (kind === "database_page" || kind === "inline_database" && !view) return i18next.t("blockHandle:database.title");
  if (kind === "inline_database") return i18next.t("blockHandle:database.header", { view });
  return blockTypeLabel(kind);
}

/**
 * The block menu of several chosen blocks, as Notion's (seen 2026-11-26 in
 * Chrome at 1441, ⌘/ in one tab and the first block's handle in another, the
 * menu read after it opened, each kind first with a line after it, a line
 * then that kind, and two of a kind): the first in the page leads it — the
 * rows its own menu has and every other chosen block takes too. Two of a kind
 * are headed as one of them is ("페이지", "데이터베이스 · 표", "코드"),
 * mixed ones "블록 N개", "블록 2개 · 표" where an inline database leads. An
 * inline database's or a full-page database's line first duplicates through
 * "복제 ›" with and without its content; a line first only "복제 ⌘D".
 * Sub-pages, full-page databases' lines or callouts all of a kind edit their
 * icons together, code blocks their language ("언어 ›", none of it checked);
 * no other of a block's own rows (open, rename, copy code, caption) stays.
 * A sub-page first names the delete "휴지통으로 이동" with no key (a line, an
 * inline database or a full-page database's line first, "삭제 Del"; seen
 * 2026-11-26 by w2775 too). Asked as the menu opens.
 */
export function blockMenuSeveral(adapter: NativeEditorGraphAdapter, selectedIds: readonly string[], close: () => void): BlockMenuSeveral | undefined {
  if (selectedIds.length < 2) return undefined;
  const roots = selectedRootsInOrder(adapter.graph, new Set(selectedIds));
  const first = roots[0];
  if (!first || roots.length < 2) return undefined;
  const kinds = roots.map((root) => severalKind(adapter, root)), lead = kinds[0]!;
  const alike = kinds.every((kind) => kind === lead);
  const viewType = lead === "inline_database" ? inlineDatabaseShownView(adapter, first)?.type : undefined;
  const view = viewType ? i18next.t(`databaseView:viewTypes.${viewType}`) : undefined;
  const editable = !adapter.readOnly;
  // A locked page keeps its icon from its own menu too (useBlockPageMenu).
  const icons = editable && alike && (lead === "callout" || lead === "child_page" || lead === "database_page")
    && roots.every((root) => root.type === "callout" || !adapter.moveContext?.pages.find((page) => page.id === childPageBlockPageId(root))?.isLocked);
  return {
    shape: blockMenuShape(kinds),
    head: alike ? ownHead(lead, view)
      : view ? i18next.t("blockHandle:menu.blockCountView", { count: roots.length, view }) : i18next.t("blockHandle:menu.blockCount", { count: roots.length }),
    deleteToTrash: lead === "child_page",
    duplicateKinds: lead === "inline_database" || lead === "database_page",
    onEditIcon: icons ? () => {
      close();
      // Its picker takes them as it opens, at once; none left over for its own menu's next opening.
      setIconCompanions(first.id, roots.slice(1).map((root) => root.id));
      if (first.type === "callout") requestCalloutIconEdit(first.id); else requestInlineDatabase(first.id, "icon");
      takeIconCompanions(first.id);
    } : undefined,
    language: editable && alike && lead === "code" ? severalLanguages(adapter, roots, close) : undefined,
  };
}

/** Several code blocks' "언어 ›": the one picked is every one's. */
function severalLanguages(adapter: NativeEditorGraphAdapter, roots: readonly GraphBlock[], close: () => void): BlockListFormat {
  const firstLanguage = roots[0]?.content.language, stored = typeof firstLanguage === "string" ? firstLanguage.trim() : "";
  return {
    formats: codeLanguageOptions(blockItemText, stored).map(({ value, label }) => [value, label] as const),
    active: null,
    onSelect: (language) => {
      for (const root of roots) {
        const current = adapter.graph.blocks.find((candidate) => candidate.id === root.id);
        if (current) adapter.updateBlock(current, { language });
      }
      rememberCodeBlockLanguage(language);
      close();
    },
  };
}
