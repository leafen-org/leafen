import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { CSSProperties, MouseEvent, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CaretRightFill, DragHandleIcon, LockIcon, SidePeekIcon } from "@/icons/leafen";
import { hasExplicitPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { usePageNames } from "@/lib/pageNames";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { libraryAgoLabel } from "@/lib/relativeTime";
import { closePagePeek } from "@/lib/pagePeek";
import { leaveSidePeek } from "@/lib/sidePeekMotion";
import { useSideRowPeekShows } from "@/lib/sideRowPeekLayout";
import { PeekHalo } from "@/components/database/PeekHalo";
import type { Page } from "@/types";
import type { LibraryPage } from "@/workspaceMembershipWire.generated";
import { librarySource, librarySourceTitle, type LibraryColumn, type LibrarySourceNames, type LibraryTreeRow } from "./libraryModel";
import panelStyles from "./LibraryPanel.module.css";
import selectionStyles from "./LibrarySelection.module.css";
import styles from "./LibraryView.module.css";
import { LibraryAddRow } from "./LibraryAddRow";
import orderStyles from "./LibraryRowOrder.module.css";
import cellStyles from "./LibraryCellChoice.module.css";
import type { LibraryCell } from "./useLibraryCellChoice";
import type { LibraryRowOrder } from "./useLibraryRowOrder";
import { pressedPoint } from "@/lib/pressedPoint";

/** Each level of an open row's pages sits one step in (Notion, seen 2026-10-02). */
const INDENT = 24;
/** The gutter left of the names that holds a row's "열기". */
const GUTTER = 24;
/**
 * The ring of a row open in the side peek stands 1px below the row's top and
 * stops at its bottom line, across the whole row, as Notion's library (seen
 * 2026-11-14 at 1440: 35px of a 37px row, 1244 wide from x 374).
 */
const LIBRARY_ROW_HALO: CSSProperties = { insetBlockStart: "1px" };

/**
 * A page with no icon, no title and nothing in it shows no mark in the
 * library, its "새 페이지" where the mark would be, as Notion's library (seen
 * 2026-11-14 in its private pages and recents: a page made and emptied with
 * ⌘Z; one with words in it or a title keeps its document mark); the sidebar
 * still draws an empty document for it.
 */
function bareRow(page: Page) {
  return !hasExplicitPageIcon(page) && page.kind !== "database" && !page.isWiki && page.hasContent === false && !(page.title ?? "").trim();
}

/**
 * The library's rows: a page's name with "열기" in the gutter — always when
 * it has pages of its own or is open, under the pointer otherwise, as
 * Notion's library (seen 2026-11-13: a page without pages, a database and a
 * database's row too) — then the shown columns; an open page's pages follow
 * one step in and end with "새 페이지 추가하기". The row whose page the side
 * peek shows wears its ring, and its "열기" reads "닫기" (LibraryRowOpen).
 */
export function LibraryRows({
  tree,
  columns,
  pages,
  sharedPageIds,
  details,
  visitedAt,
  sourceNames,
  wrapped,
  open,
  parents,
  onToggle,
  onOpenPage,
  onPeekPage,
  onAddInside,
  selected,
  onSelect,
  onRowMenu,
  onEditName,
  onEditIcon,
  order,
  chosenCell,
  indentFrom = 0,
}: {
  tree: readonly LibraryTreeRow[];
  columns: readonly LibraryColumn[];
  pages: readonly Page[];
  sharedPageIds: readonly string[];
  details: ReadonlyMap<string, LibraryPage>;
  visitedAt: ReadonlyMap<string, string>;
  sourceNames: LibrarySourceNames;
  wrapped: readonly LibraryColumn[];
  open: ReadonlySet<string>;
  /** The pages that have pages of their own. */
  parents: ReadonlySet<string>;
  onToggle: (pageId: string) => void;
  onOpenPage: (page: Page, newTab: boolean) => void;
  /** The row's "열기", which opens its page in the side peek over the library, as Notion's. */
  onPeekPage?: (page: Page) => void;
  onAddInside: (parentId: string) => void;
  /** The rows chosen, whose boxes are checked; without it the rows have no boxes. */
  selected?: ReadonlySet<string>;
  onSelect?: (pageId: string, chosen: boolean, range: boolean) => void;
  /** A row's right click, which opens the chosen pages' menu at the pointer as Notion's library. */
  onRowMenu?: (pageId: string, at: { x: number; y: number; beside?: DOMRect }) => void;
  /** How far in the top level sits, as a teamspace's pages under its row. */
  indentFrom?: number;
  /**
   * The name cell's own edits, as Notion's library (seen 2026-11-09): its name
   * or the cell's empty part renames the page over the cell and its icon opens
   * the icon picker, false where the page takes no change here; the row's
   * "열기" opens it. Without them the name opens the page.
   */
  onEditName?: (page: Page, cell: DOMRect) => boolean;
  onEditIcon?: (page: Page, at: DOMRect) => boolean;
  /** The top rows' handles, where the view is put in order by hand (useLibraryRowOrder); a handle pressed opens the row's menu. */
  order?: LibraryRowOrder;
  /** The cell chosen from the keyboard, ringed (useLibraryCellChoice). */
  chosenCell?: LibraryCell | null;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["library", "tableView", "pageHeader"]);
  // The words every row says alike, asked once for the rows, not for each.
  const changeIconLabel = t("pageHeader:changeIcon"), openRowLabel = t("tableView:openRowLabel"), closeRowLabel = t("tableView:closeRowLabel");
  const openPage = (page: Page) => (event: MouseEvent) => onOpenPage(page, event.metaKey || event.ctrlKey);
  const wrap = (column: LibraryColumn) => (wrapped.includes(column) ? "true" : undefined);
  const ringed = (page: Page, column: LibraryColumn, base: string) => (
    chosenCell?.pageId === page.id && chosenCell.column === column ? `${base} ${cellStyles.chosen}` : base);
  const person = (name: string, id?: string | null) => (name ? (
    <>
      <PersonAvatar className={styles.avatar} userId={id} name={name} size={20} />
      <span className={styles.cellText}>{name}</span>
    </>
  ) : null);

  function cell(column: LibraryColumn, page: Page): ReactNode {
    const detail = details.get(page.id);
    if (column === "creator") return person(detail?.creatorName ?? "", page.createdBy);
    if (column === "lastEditor") return person(detail?.lastEditorName ?? "", page.lastEditedBy);
    if (column === "edited") return libraryAgoLabel(page.updatedAt);
    if (column === "created") return libraryAgoLabel(page.createdAt);
    if (column === "visited") return libraryAgoLabel(visitedAt.get(page.id) || detail?.visitedAt);
    const source = librarySource(page, { pages, sharedPageIds });
    if (source.kind === "page") {
      return (
        <button type="button" className={styles.sourceButton} onClick={openPage(source.page)}>
          <PageIconGlyph page={source.page} size={16} />
          <span className={styles.cellText}>{librarySourceTitle(source, sourceNames)}</span>
        </button>
      );
    }
    const title = librarySourceTitle(source, sourceNames);
    if (!title) return null;
    return (
      <span className={styles.source}>
        {source.kind === "private" ? <LockIcon size="regular" aria-hidden="true" /> : null}
        <span className={styles.cellText}>{title}</span>
      </span>
    );
  }

  return tree.map((row, index) => {
    const indent = GUTTER + indentFrom + row.depth * INDENT;
    if (row.kind === "add") {
      return (
        <LibraryAddRow key={`${index}:add:${row.parentId}`} parentId={row.parentId} indent={indent} onAdd={() => onAddInside(row.parentId)} />
      );
    }
    const { page } = row;
    const expanded = open.has(page.id);
    const chosen = selected?.has(page.id) ?? false;
    const at = row.depth === 0 && order ? order.ids.indexOf(page.id) : -1;
    return (
      <div key={`${index}:${page.id}`} className={at >= 0 ? `${styles.row} ${orderStyles.row}` : styles.row} role="row" data-library-row={page.id}
        data-library-depth={row.depth} data-wrapped={wrapped.length > 0 ? "true" : undefined}
        data-selected={chosen ? "true" : undefined} {...(at >= 0 ? order!.reorder.row(page.id) : {})}
        onContextMenu={onRowMenu ? (event) => { event.preventDefault(); onRowMenu(page.id, pressedPoint(event)); } : undefined}>
        {at >= 0 ? (
          <button type="button" className={orderStyles.handle} aria-label={order!.label} data-library-row-handle
            {...order!.reorder.handle(page.id, true, { above: order!.ids[at - 1], below: order!.ids[at + 1] })}
            onClick={(event) => {
              const box = event.currentTarget.getBoundingClientRect();
              onRowMenu?.(page.id, { x: box.left, y: box.top, beside: box });
            }}>
            <DragHandleIcon size="regular" aria-hidden="true" />
          </button>
        ) : null}
        <PeekHalo itemId={page.id} style={LIBRARY_ROW_HALO} />
        {onSelect ? (
          <LibrarySelectBox checked={chosen} label={t("selection.select", { title: pageNames.title(page) })}
            onChange={(next, range) => onSelect(page.id, next, range)} />
        ) : null}
        <span className={ringed(page, "name", styles.nameColumn)} role="cell" data-wrap={wrap("name")} style={{ paddingInlineStart: indent }}>
          <button type="button" className={panelStyles.rowToggle} style={{ insetInlineStart: indent - GUTTER }}
            aria-label={t("openRow", { title: pageNames.title(page) })} aria-expanded={expanded}
            data-library-row-toggle={page.id} data-empty={parents.has(page.id) || expanded ? undefined : "true"}
            onClick={() => onToggle(page.id)}>
            <CaretRightFill aria-hidden="true" />
          </button>
          {onEditName ? (
            <span className={styles.nameCell} data-library-name-cell>
              {bareRow(page) ? null : (
                <button type="button" className={styles.nameIcon} aria-label={changeIconLabel}
                  onClick={(event) => { if (!onEditIcon?.(page, event.currentTarget.getBoundingClientRect())) openPage(page)(event); }}>
                  <PageIconGlyph page={page} size={18} />
                </button>
              )}
              <button type="button" className={styles.nameTitle} data-library-name
                onClick={(event) => {
                  const cell = event.currentTarget.parentElement?.getBoundingClientRect();
                  if (!cell || !onEditName(page, cell)) openPage(page)(event);
                }}>
                <span className={styles.pageTitle}><PageTitleText page={page} /></span>
              </button>
              <LibraryRowOpen pageId={page.id} label={openRowLabel} closeLabel={closeRowLabel}
                onOpen={(event) => (onPeekPage && !event.metaKey && !event.ctrlKey ? onPeekPage(page) : openPage(page)(event))} />
            </span>
          ) : (
            <button type="button" className={styles.pageButton} onClick={openPage(page)}>
              {bareRow(page) ? null : <PageIconGlyph page={page} size={18} />}
              <span className={styles.pageTitle}><PageTitleText page={page} /></span>
            </button>
          )}
        </span>
        {columns.filter((column) => column !== "name").map((column) => (
          <span key={column} className={ringed(page, column, styles.column)} role="cell" data-library-cell={column} data-wrap={wrap(column)}>
            {cell(column, page)}
          </span>
        ))}
      </div>
    );
  });
}

/**
 * A row's "열기", which reads "닫기" and closes the side peek while the peek
 * shows its page, as Notion's library (seen 2026-11-14: under the pointer
 * only, as "열기"; another row's "열기" moves the ring to that row), kept in
 * sight at the table's visible end beside the peek (.openRowDock). It follows
 * the peek itself, so a peek opening or closing draws again only the rows it
 * changes.
 */
function LibraryRowOpen({ pageId, label, closeLabel, onOpen }: {
  pageId: string;
  label: string;
  closeLabel: string;
  onOpen: (event: MouseEvent<HTMLButtonElement>) => void;
}) {
  const peeked = useSideRowPeekShows(pageId);
  return (
    <span className={styles.openRowDock}>
      <button type="button" className={styles.openRow} data-library-row-open={peeked ? "close" : ""}
        onClick={(event) => (peeked ? leaveSidePeek(closePagePeek) : onOpen(event))}>
        <SidePeekIcon size="regular" aria-hidden="true" /><span>{peeked ? closeLabel : label}</span>
      </button>
    </span>
  );
}

/**
 * A row's box in the gutter left of the table, as Notion's library (seen
 * 2026-10-07): under the pointer or while chosen; the head's selects every
 * row shown, shows "−" while only some are and clears them when all are.
 */
export function LibrarySelectBox({ checked, indeterminate = false, shown = false, label, onChange }: {
  checked: boolean;
  indeterminate?: boolean;
  /** Shown without the pointer, as the head's is while any row is chosen. */
  shown?: boolean;
  label: string;
  /** With Shift held, the rows from the last box clicked to this one take its state, as Notion's library. */
  onChange: (checked: boolean, range: boolean) => void;
}) {
  return (
    <span className={selectionStyles.selectCell} data-shown={shown || checked || indeterminate ? "true" : undefined}>
      <input type="checkbox" className={selectionStyles.selectBox} checked={checked} aria-label={label} data-library-select
        ref={(box) => { if (box) box.indeterminate = indeterminate; }}
        onClick={(event) => onChange(event.currentTarget.checked, event.shiftKey)} onChange={() => undefined} />
    </span>
  );
}

/**
 * A group's head row when the view is grouped, as a database table's (seen
 * 2026-09-30): "열기" in the gutter, which folds the group's rows, the
 * group's name and how many pages it holds.
 */
export function LibraryGroupRow({ groupKey, label, count, collapsed, onToggle }: {
  groupKey: string;
  label: string;
  count: number;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { t } = useTranslation("library");
  return (
    <div className={`${styles.row} ${panelStyles.groupRow}`} role="row" data-library-group={groupKey}>
      <span className={styles.nameColumn} role="cell" style={{ paddingInlineStart: GUTTER }}>
        <button type="button" className={panelStyles.rowToggle} style={{ insetInlineStart: 0 }} aria-label={t("openRow", { title: label })}
          aria-expanded={!collapsed} data-library-group-toggle={groupKey} onClick={onToggle}>
          <CaretRightFill aria-hidden="true" />
        </button>
        <span className={panelStyles.groupLabel}>{label}</span>
        <span className={panelStyles.groupCount}>{count}</span>
      </span>
    </div>
  );
}
