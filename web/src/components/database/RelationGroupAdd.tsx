import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { pagePlace } from "@/lib/pagePath";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { usePageNames } from "@/lib/pageNames";
import { confirmAction } from "../ConfirmActionDialog";
import { Plus } from "../icons";
import { PageIconGlyph } from "../page/PageIcon";
import { PlaceLine } from "../PlaceLine";
import { pageIconSource, RelationCreateWords } from "./PropertyRelationCellView";
import { relationPagesFound, relationPagesInOrder, useRelationPageVisits } from "./relationPageOrder";
import { useProductPagesSelect } from "../product/productPagesStore";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import { CellMenuPortal } from "./PropertyCellMenu";
import { useGroupAddPicker } from "./useGroupAddPicker";
import { capturePropertyCellRequest, type PropertyCellEnvironment } from "./propertyCellTypes";
import styles from "./databaseSheet";
import type { Page } from "@/types";
import { pagesByIdOf } from "@/lib/pageIndex";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * A view grouped by a relation: "+ 신규 그룹" lists the related database's
 * pages under a search box ("검색어를 입력하세요"), each with where it sits
 * under its title, and the one picked gets a group of its own, kept though
 * empty; one with a group already says "그룹이 이미 존재합니다.", the list
 * staying open (Notion's table, list and board, seen 2026-11-12). The list
 * hangs from the button's foot, its left on the button's, as the people's;
 * a page with nothing in it shows the empty document (pageIconSource).
 * A search names a page to make there, "+ ⛁ T에 새 〈words〉 페이지 생성", in
 * the picker's words (RelationCreateWords): the list's last line, alone and
 * highlighted when no page is found, Enter making it then; pressed, the page
 * is made in the related database under that name and gets its group
 * (Notion's, seen 2026-11-12). Its keys are a person's window's
 * (useGroupAddPicker), the focus after it ViewGroupAdd's.
 */
// The search's own line, "+ ⛁ T에 새 〈words〉 페이지 생성", among the pages' keys.
const CREATE_ROW = "\u0000create";

export function RelationGroupAdd({ environment, property, className, keepsFocus = false, has, onPick }: {
  environment: PropertyCellEnvironment;
  property: { config?: unknown; dataSourceId?: string; databaseId?: string };
  className: string;
  /** Whether its button takes no focus when pressed, its closing giving the focus back to what held it (ViewGroupAdd). */
  keepsFocus?: boolean;
  has(pageId: string): boolean;
  onPick(pageId: string): void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["boardView", "tableView", "propertyCell"]);
  const { snapshot, operations, context } = environment;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const targetId = String((property.config as { relationDatabaseId?: unknown } | undefined)?.relationDatabaseId ?? property.dataSourceId ?? property.databaseId);
  // Where each sits, as a search lists it: the reader's pages and, above them, the path the
  // server names — the database and the pages holding it — as Notion's "Top / ... / database"
  // (seen 2026-11-12). The view's own stand-in for the database (its title alone) would end the path.
  const appPages = useProductPagesSelect((pages) => pages?.pages);

  useEffect(() => {
    if (!open) return;
    // Its search takes the keys. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: a board grouped by a
    // relation, "+ 신규 그룹" pressed, "z" went into the search (Leafen by code, not seen).
    const stopCaret = focusOpenedField(() => searchRef.current);
    const request = capturePropertyCellRequest(context);
    void operations.loadRelation(targetId, { rows: true, signal: request.signal }, context);
    return () => { stopCaret(); request.abort(); };
  }, [context, open, operations, targetId]);

  const placesById = useMemo(() => pagesByIdOf(appPages ?? []) as unknown as Record<string, Page>, [appPages]);
  // In the relation picker's order and found as it finds them (relationPageOrder.ts): Notion's
  // lists are one search of the database (seen 2026-11-12), the pages the reader visited first.
  const visited = useRelationPageVisits(open, context.workspaceId);
  const ordered = useMemo(() => !open ? [] : relationPagesInOrder(Object.values(snapshot.pagesById)
    .filter((page) => page.parentType === "database" && page.parentId === targetId && !page.inTrash), visited), [open, snapshot.pagesById, targetId, visited]);
  const pages = useMemo(() => relationPagesFound(ordered, query, pageNames.title).slice(0, 50), [ordered, query, pageNames]);
  usePathsAboveShown(pages, placesById);
  const words = query.trim(), targetDb = snapshot.pagesById[targetId];
  const canCreate = words.length > 0 && targetDb?.kind === "database";
  const rows = useMemo(() => [...pages.map((page) => ({ id: page.id, label: pageNames.title(page) })),
    ...canCreate ? [{ id: CREATE_ROW, label: words }] : []], [canCreate, pages, words, pageNames]);
  // Made in the database the view shows (a relation to itself), the page changes the view's rows
  // and with them its cells' context and its group writer; what follows the making goes by the
  // view as it is then, as a relation cell's "새로 만들기" does (PropertyRelationCell).
  const shown = useRef({ context, onPick });
  shown.current = { context, onPick };
  const picker = useGroupAddPicker({ open, rows, query, onQuery: setQuery, onClose: close, inputRef: searchRef, listRef,
    label: t("tableView:searchRelatedPage"), onPick: (row) => row.id === CREATE_ROW ? void createPage(words) : pick(row.id) });

  function close() {
    setOpen(false);
    setQuery("");
  }

  function pick(pageId: string) {
    if (has(pageId)) { void confirmAction({ message: t("tableView:groupExists"), confirmLabel: t("tableView:groupExistsOk"), cancelLabel: null }); return; }
    close();
    onPick(pageId);
  }

  // Made, named, then its group added, the window closed at once — in the
  // view's own database too (Notion's, seen 2026-11-12: "S에서 마바 페이지
  // 생성" on a board and a table grouped by S's relation to itself makes
  // "마바" under no value and its group).
  async function createPage(title: string) {
    close();
    const request = capturePropertyCellRequest(context);
    try {
      const made = await operations.createRelatedRow(targetId, { signal: request.signal }, context);
      if (!made || request.signal.aborted) return;
      await operations.renameRelatedRow(made.id, title, shown.current.context);
      if (!request.signal.aborted) shown.current.onPick(made.id);
    } finally {
      request.abort();
    }
  }

  return (
    <>
      <button ref={triggerRef} type="button" className={className} aria-haspopup="dialog" aria-expanded={open}
        {...(keepsFocus ? { "data-press-keeps-focus": "" } : {})} onClick={() => setOpen(true)}>
        <Plus size="small" aria-hidden="true" />{t("boardView:newGroup")}
      </button>
      {open && (
        <CellMenuPortal triggerRef={triggerRef} onClose={close} ariaLabel={t("boardView:newGroup")} className={styles.peopleMenu}
          backdropClassName={styles.menuBackdrop} width={240} gap={0} halfHigh
          onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); close(); } }}>
          <div className={styles.peopleSearchBox}>
            <input {...picker.inputProps} ref={searchRef} className={styles.peopleSearch} type="text"
              placeholder={t("tableView:searchRelatedPage")} aria-label={t("tableView:searchRelatedPage")} />
          </div>
          <div {...picker.listBoxProps} ref={listRef} className={styles.peopleList}>
            {pages.map((page) => (
              <NativePickerOption key={page.id} itemKey={page.id} state={picker.state} tabIndex={-1} className={`${styles.peopleOption} ${styles.pageOption}`}>
                {({ labelProps }) => <>
                  <span className={styles.pageOptionIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(page)} size={18} /></span>
                  <span className={styles.peopleOptionText}>
                    <span {...labelProps}>{pageNames.title(page)}</span>
                    <PlaceLine place={pagePlace(page as unknown as Page, placesById)} />
                  </span>
                </>}
              </NativePickerOption>
            ))}
            {canCreate ? (
              <NativePickerOption itemKey={CREATE_ROW} state={picker.state} tabIndex={-1} className={`${styles.peopleOption} ${styles.relationGroupCreate}`}>
                {() => <>
                  <Plus size="regular" aria-hidden="true" />
                  <RelationCreateWords title={words} database={targetDb} />
                </>}
              </NativePickerOption>
            ) : null}
          </div>
        </CellMenuPortal>
      )}
    </>
  );
}
