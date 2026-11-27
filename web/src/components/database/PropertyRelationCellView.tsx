import { pageIconSource, usePageNames } from "@/lib/pageNames";
import { type HTMLAttributes, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type ReactNode, type RefObject, memo, useContext, useEffect, useRef, useState } from "react";
import { useDragReorder } from "@/lib/useDragReorder";
import { Trans, useTranslation } from "react-i18next";
import type { DbProperty, Page } from "@/lib/types";
import { Plus } from "../icons";
import { DragHandleIcon, MinusIcon, RenameInPlaceIcon } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { RelationPickerFilter, type RelationPickerScope } from "./RelationPickerFilter";
import { PageIconGlyph } from "../page/PageIcon";
import { CellMenuPortal, openEditorFromCellBackground } from "./PropertyCellMenu";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";
import styles from "./databaseSheet";
import { NativePickerOption, type useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { RelationSectionContext, RelationSectionFoot, type RelationNewPage } from "./relationSection";
import { RelationSectionLineAdd, relationSectionLineClass } from "./relationSectionLine";
import { useRelationChipsDrawn } from "./relationChipsDrawn";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import { type PickerWindowLine, pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "../shell/PhoneSheet";
import { PickerOpened } from "./PropertySelectPicker";
import sheet from "./PropertyRelationSheet.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// The keys the search's list answers while the window holds the focus.
const PICKER_KEYS = new Set(["ArrowDown", "ArrowUp", "PageDown", "PageUp", "Enter"]);
// A row page's lines show this many pages, then "외 N개 항목" (Notion's, seen 2026-11-14).
const ROW_PAGE_LINES = 5;

export { pageIconSource };

export interface PropertyRelationCellViewProps {
  /** "···": which properties the rows show. */
  properties: ReactNode;
  values(page: Page): ReactNode;
  scope: RelationPickerScope;
  onScope(scope: RelationPickerScope): void;
  row: Page;
  prop: DbProperty;
  targetDb?: Page;
  /** A row's name here: a task database's unnamed row reads "새 작업". */
  titleOf(page: Page): string;
  /** On a row's page: each page a line of its own, pressed as one to open it. */
  rowDetail?: boolean;
  selectedIds: string[];
  selectedPages: Page[];
  selectedSet: Set<string>;
  pendingSelectedCount: number;
  missingSelectedCount: number;
  /** Of those, the rows the reader may not see. */
  unreadableSelectedCount: number;
  filtered: Page[];
  /** The pages shown chosen, then the rest, in the list's order. */
  chosen: Page[];
  others: Page[];
  ordinaryTargetRowsPending: boolean;
  ordinaryTargetRowsFailed: boolean;
  canCreate: boolean;
  open: boolean;
  query: string;
  triggerRef: RefObject<HTMLButtonElement | null>;
  listRef: RefObject<HTMLDivElement | null>;
  popoverRef: RefObject<HTMLDivElement | null>;
  inputRef: RefObject<HTMLInputElement | null>;
  picker: ReturnType<typeof useNativeAutocomplete<{ id: string; label: string }>>;
  openMenu: () => void;
  closeMenu: (restoreFocus?: boolean) => void;
  openPage: (pageId: string, event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) => void;
  /** A page section's "새 페이지 추가하기": makes a page of the related database, related here, its title typed after. */
  startPage?: () => RelationNewPage;
  /** Takes a page off the relation. */
  removePage?: (pageId: string) => void;
  /** A page section's "페이지 이름 바꾸기": the page's new name. */
  renamePage?: (pageId: string, title: string) => void;
  /** Moves a related page before or after another, by its ⠿. */
  reorderPage?: (pageId: string, targetId: string, side: "before" | "after") => void;
}

type ChipWays = {
  open(pageId: string, event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>): void;
  remove?(pageId: string): void;
  /** Opens the picker over the chip's line, from its "+". */
  linkFrom(chip: HTMLElement | null): void;
};

/**
 * A chosen page's chip outside a page section: drawn again for its page and
 * name alone, its presses going by the cell as last drawn (`ways`). A row's
 * peek with a relation of 100 pages drew its 1,400 parts again each time its
 * cell was (seen 2026-11-13).
 */
const RelationChip = memo(function RelationChip({ page, title, line, removable, ways }: {
  page: Page; title: string; line: boolean; removable: boolean; ways: RefObject<ChipWays>;
}) {
  const { t } = useTranslation("propertyCell");
  return (
    <span className={styles.relationChip} data-row-relation-chip
      {...(line ? { role: "button", tabIndex: 0, "aria-label": t("open", { name: title }),
        onClick: (event: ReactMouseEvent<HTMLElement>) => ways.current.open(page.id, event),
        onKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => {
          if (event.target === event.currentTarget && (fieldEnter(event) || event.key === " ")) ways.current.open(page.id, event);
        } } : {})}>
      <span className={styles.relationPageIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(page)} size={18} /></span>
      <span className={styles.relationChipLink} data-relation-page-name
        onClick={line ? undefined : (event) => ways.current.open(page.id, event)}>
        <span className={styles.relationChipName}><TitleWithCustomEmoji text={title} named={page} workspaceId={page.workspaceId} /></span>
      </span>
      {/* A row page's line takes its page off or links another from a box at its end under the
          pointer (Notion's "−" and "+", seen 2026-11-12). */}
      {line ? <span data-relation-section-row-actions>
        {removable ? <button type="button" aria-label={t("unlinkPage")} title={t("unlinkPage")} data-relation-unlink
          onClick={(event) => { event.stopPropagation(); ways.current.remove?.(page.id); }}><MinusIcon size="small" aria-hidden="true" /></button> : null}
        <button type="button" aria-label={t("linkAnotherPage")} title={t("linkAnotherPage")} data-relation-link-another
          onClick={(event) => {
            event.stopPropagation();
            ways.current.linkFrom(event.currentTarget.closest<HTMLElement>("[data-row-relation-chip]"));
          }}><Plus size="small" aria-hidden="true" /></button>
      </span> : null}
    </span>
  );
});

export function PropertyRelationCellView(props: PropertyRelationCellViewProps) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["propertyCell", "common", "blockItem", "propertyTypes"]);
  const { touch } = useSidebarLayout(true);
  const readOnly = useCellReadOnly();
  const withinWords = t("propertyCell:relationSearchWithin", { database: "\u0000" }).split("\u0000");
  const section = useContext(RelationSectionContext);
  // A row page's line of its own: not a page section's, which its own lines draw.
  const line = Boolean(props.rowDetail && !section);
  const [renaming, setRenaming] = useState<string | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  // A long relation draws its first pages as it shows and the rest after,
  // behind any press (relationChipsDrawn.ts): a row's peek holding 100 drew
  // them all in the frame that opened it, some 1,500 parts — its lines run
  // past the peek's height, a cell's past its width (seen 2026-11-13).
  const drawnChips = useRelationChipsDrawn(props.selectedPages.length);
  // A row page's line of its own shows its first pages, the rest behind "외 N개 항목".
  const linesShown = line ? Math.min(drawnChips, ROW_PAGE_LINES) : drawnChips;
  const linesHidden = line ? Math.max(0, props.selectedPages.length - ROW_PAGE_LINES) : 0;
  // What a page's chip does, as this cell is now drawn (RelationChip).
  const chipWays = useRef<ChipWays>(null!);
  chipWays.current = {
    open: props.openPage,
    remove: props.removePage,
    linkFrom(chip) {
      lineRef.current = chip;
      setFromLine(true);
      props.openMenu();
    },
  };
  // A line's "+" opens the picker over that line, searching the linked pages
  // (Notion's "연결된 페이지 검색", seen 2026-11-12); the value or a cell opens it over the value.
  const lineRef = useRef<HTMLElement | null>(null);
  const [fromLine, setFromLine] = useState(false);
  useEffect(() => {
    if (props.open) return;
    lineRef.current = null;
    setFromLine(false);
  }, [props.open]);
  // The chosen pages move by the ⠿ at their start, in the picker's chosen
  // rows and a page section's lines, as Notion's (seen 2026-11-07); the order
  // is the relation's own.
  const reorder = useDragReorder(`relation-pages:${props.row.id}:${props.prop.id}`,
    (pageId, targetId, side) => props.reorderPage?.(pageId, targetId, side), { live: true });
  const ordered = props.selectedPages.map(({ id }) => id);
  const handle = (pageId: string) => {
    const at = ordered.indexOf(pageId);
    return <span className={styles.relationDragHandle} data-relation-drag-handle aria-label={t("propertyCell:movePage")} role="button" tabIndex={-1}
      {...reorder.handle(pageId, Boolean(props.reorderPage), { above: ordered[at - 1], below: ordered[at + 1] })}
      onPointerDown={(event) => event.stopPropagation()} onClick={(event) => { event.preventDefault(); event.stopPropagation(); }}>
      <DragHandleIcon size="small" aria-hidden="true" /></span>;
  };
  const movable = Boolean(props.reorderPage);
  // What the section's "새 페이지 추가하기" does, which its lines' "+" does too.
  const startLine = useRef<(() => void) | null>(null);
  // While a search is typed the chosen pages stand as any other line, 32
  // high and 1 apart, without their ⠿, "추가 선택" as far below the last of
  // them as ever, as Notion's (seen 2026-11-12).
  const searching = props.query.trim().length > 0, pickMoves = movable && !searching;
  // A relation limited to one page heads its page "선택함" and the rest "선택
  // 변경", a page picked there taking its place, as Notion's (seen 2026-11-16).
  const single = props.prop.config?.limit === 1;
  // The chosen pages, "추가 선택" once the chosen ones are listed, then the rest: a line each.
  const pages = props.chosen.length + props.others.length, more = props.chosen.length > 0 && props.others.length > 0 ? props.chosen.length : -1;
  const pageOf = (line: number) => more >= 0 && line > more ? line - 1 : line;
  const pageAt = (index: number) => index < props.chosen.length ? props.chosen[index] : props.others[index - props.chosen.length];
  const focused = props.picker.active;
  const reorderable = (page: Page) => props.selectedSet.has(page.id) && pickMoves;
  // A page's line is 32 high, each 1 apart (`.relationList`); a chosen page's 31, its page reaching
  // over that 1, so the chosen ones touch, and "추가 선택" 35 with the space around it — 8 below the
  // last chosen page and 4 above the rest, as Notion's (seen 2026-11-11, `.relationGroupLine`).
  const rows = usePickerWindow({ scrollRef: props.listRef, open: props.open, count: pages + (more < 0 ? 0 : 1),
    keyAt: (line) => line === more ? "more" : pageAt(pageOf(line)).id,
    // A touch screen's sheet draws each page a 45px row, as Notion's phone (seen 2026-11-15 at 402pt).
    sizeAt: (line) => line === more ? searching ? 34 : 35 : touch ? 45 : !searching && pageOf(line) < props.chosen.length ? 31 : 32, gap: touch ? 0 : 1,
    focused: focused < 0 || focused >= pages ? -1 : more >= 0 && focused >= more ? focused + 1 : focused });
  const drawLine = (line: PickerWindowLine) => {
    if (line.index === more) {
      const placed = line.top === undefined ? undefined : pickerWindowLine(line, rows.measure);
      return <div key="more" role="presentation" {...placed} className={styles.relationGroupLine}>
        <div className={styles.relationGroup}>{t(single ? "propertyCell:pagesChange" : "propertyCell:pagesMore")}</div></div>;
    }
    const index = pageOf(line.index), page = pageAt(index), chosenLine = index < props.chosen.length && !searching;
    const chosen = props.selectedSet.has(page.id);
    const option = (
      <NativePickerOption key={page.id} itemKey={page.id} state={props.picker.state} className={styles.relationOption}
        aria-selected={chosen} data-relation-index={index} data-selected={chosen ? "true" : undefined}
        aria-setsize={rows.windowed ? pages : undefined} aria-posinset={rows.windowed ? index + 1 : undefined}>{({ labelProps }) => <>
        {chosen && pickMoves ? handle(page.id) : null}
        <span className={styles.relationPageIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(page)} size={touch ? 22 : 18} /></span>
        <span {...labelProps} className={styles.relationOptionTitle}>{props.titleOf(page)}</span>
        {props.values(page)}
        <span className={styles.relationRowAction} aria-hidden="true" title={t(chosen ? "propertyCell:unlinkPage" : "propertyCell:linkPage")}>
          {chosen ? <MinusIcon size="small" /> : <Plus size="small" />}
        </span>
      </>}</NativePickerOption>
    );
    if (line.top === undefined && !reorderable(page) && !chosenLine) return option;
    const placed = line.top === undefined ? undefined : pickerWindowLine(line, rows.measure);
    const dragged = reorderable(page) ? reorder.row(page.id, (source) => ordered.includes(source)) : undefined;
    // A line that both moves by its ⠿ and stands in the window takes both refs.
    const ref = dragged && placed?.ref ? (element: HTMLDivElement | null) => { dragged.ref(element); placed.ref!(element); } : dragged?.ref ?? placed?.ref;
    return <div key={page.id} role="presentation" className={chosenLine ? styles.relationChosenLine : undefined}
      {...placed} {...dragged} ref={ref}>{option}</div>;
  };
  // A search names a page to make in the related database, "⛁ T에 새 〈words〉 페이지 생성": the
  // only line under "결과 없음" when no page is found, else on a strip under the list, the
  // pages still listed, as Notion's (seen 2026-11-12). Its "검색 피드백 보내기" is Notion's
  // own service and stays out.
  const unfound = props.canCreate && props.filtered.length === 0;
  const create = props.canCreate ? (
    <NativePickerOption itemKey="__create" state={props.picker.state} className={styles.relationCreate}
      aria-selected={false} data-relation-index={props.filtered.length}>{({ labelProps }) => <>
      <Plus size="regular" aria-hidden="true" />
      <RelationCreateWords title={props.query.trim()} database={props.targetDb} labelProps={labelProps} />
    </>}</NativePickerOption>
  ) : null;
  const panel = (
    <>
    {/* "입력 값 지우기" leaves the search for the window itself: what is typed next goes
        nowhere, the arrows and Enter still move and pick, Escape closes, as Notion's (seen
        2026-11-12, its focus on the page). */}
    <div ref={panelRef} className={styles.relationPanel} tabIndex={-1} onKeyDown={(event) => {
      if (event.target !== event.currentTarget || !PICKER_KEYS.has(event.key) || imeComposing(event)) return;
      event.preventDefault();
      props.inputRef.current?.dispatchEvent(new KeyboardEvent("keydown", { key: event.key, bubbles: true, cancelable: true }));
    }}>
    <div className={styles.relationSearchBar} data-relation-search-bar="">
      <input {...props.picker.inputProps} ref={props.inputRef} className={styles.relationSearch} autoFocus={fieldFocusOnOpen()}
        placeholder={t(fromLine ? "propertyCell:searchLinkedPages" : "propertyCell:linkOrCreatePage")} />
      {/* Notion's shared "입력 값 지우기" at the search's end while it holds words (seen 2026-11-12). */}
      <SearchClearButton value={props.query} className={styles.relationSearchClear}
        onClear={() => { props.picker.state.setInputValue(""); panelRef.current?.focus({ preventScroll: true }); }} />
      {/* Notion's phone sheet holds the search alone (seen 2026-11-15 at 402pt). */}
      {touch ? null : <RelationPickerFilter scope={props.scope} onScope={props.onScope} />}
      {props.targetDb && !touch ? (
        <span className={styles.relationSearchScope} data-relation-search-scope>
          <span className={styles.relationScopeIcon}><PageIconGlyph page={{ icon: props.targetDb.icon ?? "", iconType: props.targetDb.iconType, kind: "database" }}
            size={18} database="cylinder" /></span>
          <span>{withinWords[0]}<b>{pageNames.database(props.targetDb)}</b>{withinWords[1]}</span>
        </span>
      ) : null}
    </div>
    <div {...props.picker.listBoxProps} className={styles.relationList} ref={props.listRef} aria-multiselectable="true" data-relation-list=""
      data-chosen={props.chosen.length > 0 ? "true" : undefined} data-searching={searching ? "true" : undefined}
      data-unfound={unfound ? "true" : undefined}>
      {props.chosen.length > 0 || props.others.length > 0 || unfound ? (
        <div className={styles.relationGroup} role="presentation" data-relation-group="">
          {unfound ? t("propertyCell:relationNoResults")
            : props.chosen.length > 0 ? single ? t("propertyCell:pagesChosenOne") : t("propertyCell:pagesChosen", { count: props.chosen.length })
              : t("propertyCell:pagesPick")}
          {props.properties}
        </div>
      ) : null}
      {rows.windowed ? (
        <div ref={rows.linesRef} role="presentation" style={pickerWindowBox(rows.height)}>
          {rows.lines.map(drawLine)}
        </div>
      ) : rows.lines.map(drawLine)}
      {props.filtered.length === 0 && !props.query.trim() && (
        <div className={styles.selectEmpty}>
          {props.ordinaryTargetRowsPending
            ? t("propertyCell:relationLoading")
            : props.ordinaryTargetRowsFailed
              ? t("propertyCell:relationDatabaseUnavailable")
              : props.targetDb ? t("propertyCell:noPagesYet") : t("propertyCell:relationDatabaseUnavailable")}
        </div>
      )}
      {props.filtered.length === 0 && props.query.trim() && !props.canCreate && <div className={styles.selectEmpty}>{t("propertyCell:noMatchingPages")}</div>}
      {unfound ? create : null}
    </div>
    {/* Found pages leave the new page to a strip of its own under the list, as Notion's. */}
    {props.canCreate && !unfound ? <div className={styles.relationCreateStrip}>{create}</div> : null}
    </div>
    </>
  );
  return (
    <div className={styles.relationCell}>
      <div
        className={styles.relationChips}
        role="group"
        aria-label={props.prop.name}
        data-empty={props.selectedIds.length === 0 ? "true" : undefined} data-relation-section={section ? "true" : undefined}
        onClick={(event) => openEditorFromCellBackground(event, props.openMenu)}
      >
        {props.selectedPages.length > 0 && <span className={styles.valueMark}>{props.selectedPages.map((page, index) => !section ? index >= linesShown ? null : (
          <RelationChip key={page.id} page={page} title={props.titleOf(page)} line={line} removable={Boolean(props.removePage)} ways={chipWays} />
        ) : (
          <span key={page.id} className={section ? `${styles.relationChip} ${relationSectionLineClass}` : styles.relationChip} data-row-relation-chip
            {...(section && movable ? reorder.row(page.id, (source) => ordered.includes(source)) : {})}
            {...(line ? { role: "button", tabIndex: 0, "aria-label": t("propertyCell:open", { name: props.titleOf(page) }),
              onClick: (event: ReactMouseEvent<HTMLElement>) => props.openPage(page.id, event),
              onKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => {
                if (event.target === event.currentTarget && (fieldEnter(event) || event.key === " ")) props.openPage(page.id, event);
              } } : {})}>
            {section && props.startPage && !readOnly && renaming !== page.id ? <RelationSectionLineAdd startFrom={startLine} /> : null}
            {section && movable && renaming !== page.id ? handle(page.id) : null}
            <span className={styles.relationPageIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(page)} size={18} /></span>
            {renaming === page.id ? (
              <input data-relation-section-rename autoFocus defaultValue={props.titleOf(page)} placeholder={t("propertyCell:newPageTitle")}
                aria-label={t("propertyCell:renamePage")} onClick={(event) => event.stopPropagation()}
                onFocus={(event) => { const end = event.currentTarget.value.length; event.currentTarget.setSelectionRange(end, end); }}
                onKeyDown={(event) => {
                  if (fieldEnter(event)) { event.preventDefault(); event.currentTarget.blur(); }
                  if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); event.currentTarget.value = props.titleOf(page); event.currentTarget.blur(); }
                }}
                onBlur={(event) => {
                  const title = event.currentTarget.value.trim();
                  setRenaming(null);
                  if (title !== props.titleOf(page).trim()) props.renamePage?.(page.id, title);
                }} />
            ) : <span className={styles.relationChipLink} data-relation-page-name
              onClick={line ? undefined : (event) => props.openPage(page.id, event)}>
              <span className={styles.relationChipName}><TitleWithCustomEmoji text={props.titleOf(page)} named={page} workspaceId={page.workspaceId} /></span>
            </span>}
            {/* A page section's line shows the properties its "⋯" picks after the name (Notion's, seen 2026-11-07). */}
            {section ? <span data-relation-section-values>{props.values(page)}</span> : null}
            {/* A page section's line takes its page off, or renames it where it stands, from a box at its end
                (Notion's "−" and "페이지 이름 바꾸기", seen 2026-11-07); the box goes while the name is typed. */}
            {/* A row page's line takes its page off or links another from a box at its end under the
                pointer (Notion's "−" and "+", seen 2026-11-12). */}
            {line ? <span data-relation-section-row-actions>
              {props.removePage ? <button type="button" aria-label={t("propertyCell:unlinkPage")} title={t("propertyCell:unlinkPage")} data-relation-unlink
                onClick={(event) => { event.stopPropagation(); props.removePage?.(page.id); }}><MinusIcon size="small" aria-hidden="true" /></button> : null}
              <button type="button" aria-label={t("propertyCell:linkAnotherPage")} title={t("propertyCell:linkAnotherPage")} data-relation-link-another
                onClick={(event) => {
                  event.stopPropagation();
                  lineRef.current = event.currentTarget.closest<HTMLElement>("[data-row-relation-chip]");
                  setFromLine(true);
                  props.openMenu();
                }}><Plus size="small" aria-hidden="true" /></button>
            </span> : null}
            {section && renaming !== page.id && (props.removePage || props.renamePage) ? <span data-relation-section-row-actions>
              {props.removePage ? <button type="button" aria-label={t("propertyCell:unlinkPage")} title={t("propertyCell:unlinkPage")} data-relation-unlink
                onClick={(event) => { event.stopPropagation(); props.removePage?.(page.id); }}><MinusIcon size="small" aria-hidden="true" /></button> : null}
              {props.renamePage ? <button type="button" aria-label={t("propertyCell:renamePage")} title={t("propertyCell:renamePage")} data-relation-rename-in-place
                onClick={(event) => { event.stopPropagation(); setRenaming(page.id); }}><RenameInPlaceIcon size="small" aria-hidden="true" /></button> : null}
            </span> : null}
          </span>
        ))}</span>}
        {/* "외 N개 항목" under them, pressed for the picker over the value searching the linked pages,
            as Notion's (seen 2026-11-14: 5 of 100 pages, a 32px line of 14px secondary words). */}
        {linesHidden > 0 ? (
          <span className={styles.relationMoreLine} role="button" tabIndex={0} data-relation-more-line
            onClick={(event) => { event.stopPropagation(); chipWays.current.linkFrom(event.currentTarget.parentElement?.querySelector<HTMLElement>("[data-row-relation-chip]") ?? null); }}
            onKeyDown={(event) => {
              if (event.target !== event.currentTarget || (!fieldEnter(event) && event.key !== " ") || imeComposing(event)) return;
              event.preventDefault();
              chipWays.current.linkFrom(event.currentTarget.parentElement?.querySelector<HTMLElement>("[data-row-relation-chip]") ?? null);
            }}>{t("propertyCell:moreLinkedPages", { count: linesHidden })}</span>
        ) : null}
        {props.pendingSelectedCount > 0 && <span className={styles.relationChipSkeleton} data-row-relation-loading aria-label={t("propertyCell:relationLoading")} />}
        {props.unreadableSelectedCount > 0
          ? <span className={styles.relationMissingChip} data-row-relation-missing="no-access">{t("propertyCell:noAccess")}</span>
          : props.missingSelectedCount > 0 && <span className={styles.relationMissingChip} data-row-relation-missing aria-label={t("propertyCell:relationUnavailable")}>{t("propertyCell:unavailable")}</span>}
        {/* A touch screen has no pointer to bring a line's "+": a row page's value ends in a
            "+ 페이지 추가" line of its own instead, as Notion's iPad and iPhone (seen 2026-11-13). */}
        {line && !readOnly && props.selectedPages.length > 0 ? (
          <button type="button" className={styles.relationAddLine} data-relation-add-line
            onClick={(event) => { event.stopPropagation(); props.openMenu(); }}>
            <Plus size="wide" aria-hidden="true" />{t("propertyCell:addPage")}
          </button>
        ) : null}
        {props.selectedIds.length === 0 && !section && <span className={styles.cellEmpty}>&nbsp;</span>}
        {section ? (
          <RelationSectionFoot triggerRef={props.triggerRef} open={props.open} onLink={props.openMenu} onCreate={props.startPage} name={props.prop.name} startFrom={startLine}
            properties={props.properties} />
        ) : (
          <button
            ref={props.triggerRef}
            type="button"
            className={styles.propertyCellEditTrigger}
            aria-label={t("propertyCell:editRelation", { name: props.prop.name })}
            aria-haspopup="dialog"
            aria-expanded={props.open}
            {...readOnlyStop(readOnly)}
            onClick={props.openMenu}
          >
            <Plus size="tiny" aria-hidden="true" />
          </button>
        )}
      </div>
      {/* A touch screen's is Notion's phone sheet "관계형", named for the kind of property (seen 2026-11-15 at
          402pt in iOS Safari): half the screen, the search a 45px group of its own (16px, not focused), then
          "페이지 선택" over the pages, each a 45px row of its 22px icon and its 17px title. */}
      {props.open && touch ? (
        <TouchSheetLayout>
          <PhoneSheet size="half" title={t("propertyTypes:labels.relation")} label={t("propertyCell:editRelationProperty")} tone="settings"
            onClose={() => props.closeMenu(true)} sheetRef={props.popoverRef} data={{ "data-relation-sheet": "" }}>
            <div className={sheet.sheet}>{panel}</div>
          </PhoneSheet>
          <PickerOpened />
        </TouchSheetLayout>
      ) : props.open ? (
        <CellMenuPortal
          popoverRef={props.popoverRef}
          triggerRef={props.triggerRef}
          onClose={() => props.closeMenu(true)}
          ariaLabel={t("propertyCell:editRelationProperty")}
          className={styles.relationMenu}
          backdropClassName={styles.menuBackdrop}
          width={560}
          scroll={false}
          cover
          coverRef={fromLine ? lineRef : undefined}
          // Its side is chosen for its full 336 (`.relationPanel`), as Notion's (seen 2026-11-08).
          sideBy={336}
          tabLeavesCell
        >
          {panel}
        </CellMenuPortal>
      ) : null}
    </div>
  );
}

/**
 * "⛁ T에 새 〈words〉 페이지 생성": the page a search names, made in the related
 * database — the relation picker's and a relation group's "+ 신규 그룹" say it
 * alike, as Notion's (seen 2026-11-12), and a phone's relation filter, its ⛁
 * 24 and the name 2 after it (`phone`, seen 2026-11-17 on iPhone Safari at 402).
 */
export function RelationCreateWords({ title, database, labelProps, phone }: { title: string; database?: Page; labelProps?: HTMLAttributes<HTMLElement>; phone?: boolean }) {
  const { t } = useTranslation("propertyCell");
  return <span {...labelProps} className={styles.relationCreateText}>
    <Trans t={t} i18nKey="propertyCell:newRelationParts" values={{ title }} components={{
      m: <span className={styles.relationCreateQuiet} />, name: <span className={styles.relationCreateName} />,
      db: <RelationCreateDatabase page={database} phone={phone} /> }} />
  </span>;
}

/** The related database a new page goes in, as its search names it: its icon and name. */
function RelationCreateDatabase({ page, phone }: { page?: Page; phone?: boolean }) {
  const pageNames = usePageNames();
  if (!page) return null;
  return <span className={styles.relationCreateDatabase} data-phone={phone ? "true" : undefined}>
    <span className={styles.relationScopeIcon}><PageIconGlyph page={{ icon: page.icon ?? "", iconType: page.iconType, kind: "database" }}
      size={phone ? 21.6 : 18} database="cylinder" /></span>
    <span>{pageNames.database(page)}</span>
  </span>;
}
