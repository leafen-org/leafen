import { memo, useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, RelationIcon, X, XCircleIcon } from "@/icons/leafen";
import { PageIcon, type PageIconSource } from "@/components/page/PageIcon";
import { usePageNames } from "@/lib/pageNames";
import type { AutomationAction, AutomationValueExpression, DbProperty, EditPropertyAutomationAction, Page } from "@/lib/types";
import type { AutomationEditorSurface } from "./automationActionModel";
import { ListedItems, ValueModeButton, automationLiteralInMode, automationValueModeOf } from "./AutomationPersonValue";
import { useAutomationRelationPages } from "./automationRelationRows";
import { relationPagesFound, useRelationRowsInOrder } from "@/components/database/relationPageOrder";
import personStyles from "./AutomationPersonValue.module.css";
import styles from "./AutomationRelationValue.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { type PickerWindowLine, pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";

/** The page a run starts from, as a relation value names it on each run: Notion's "페이지 실행". */
export type AutomationRelationSource = "trigger_page";
/** A relation value's pages: one by its ID, or the page the run starts from. */
export type AutomationRelationItem = string | { source: AutomationRelationSource };

/** A page no longer among the related database's: an empty page's mark, as one without an icon or a body. */
const NO_PAGE: PageIconSource = { icon: "", iconType: "none", hasContent: false };
const iconSource = (page: Page | undefined): PageIconSource => page
  ? { id: page.id, icon: page.icon ?? "", iconType: page.iconType, kind: page.kind, hasContent: page.hasContent } : NO_PAGE;

const itemKey = (item: AutomationRelationItem) => typeof item === "string" ? `page:${item}` : `source:${item.source}`;
/** The window's "페이지 실행" row, one item for every drawing so its row is drawn again only when it changes. */
const TRIGGER_PAGE: AutomationRelationItem = { source: "trigger_page" };

/** A relation value's pages as written, the page a run starts from among them, in the order picked. */
export function automationRelationItemsOf(value: AutomationValueExpression): AutomationRelationItem[] {
  if (value.kind !== "literal" || !Array.isArray(value.value)) return [];
  return value.value.flatMap((item): AutomationRelationItem[] => typeof item === "string" ? [item]
    : item && typeof item === "object" && (item as { source?: unknown }).source === "trigger_page" ? [{ source: "trigger_page" }] : []);
}

/**
 * Whether a relation value may name the page a run starts from, as Notion's
 * "페이지 실행" (seen 2026-11-22): where a run has a page — a database's
 * automation or button — and the relation points to the database it is in.
 */
export function relationTakesTriggerPage(property: DbProperty, surface: AutomationEditorSurface | undefined) {
  const target = property.config?.relationDatabaseId ?? property.databaseId;
  return (surface === "event_automation" || surface === "database_button") && target === property.databaseId;
}

/**
 * The related database's pages, as the automation's view reads them; in the
 * relation picker's order (useRelationRowsInOrder) where they are `listed`:
 * Notion's value window lists them as its relation cell's picker does, the
 * last edited first (seen 2026-11-22 at 1440 dark, seven pages edited a
 * minute apart: the window, the cell's picker and a condition's list alike).
 */
function useRelationPages(property: DbProperty, listed = false) {
  const rows = useAutomationRelationPages(property.config?.relationDatabaseId ?? property.databaseId);
  const inOrder = useRelationRowsInOrder(rows, rows?.[0]?.workspaceId, listed);
  return listed ? inOrder : rows ?? NO_PAGES;
}

const NO_PAGES: Page[] = [];

/** The pages by their IDs, made again only when the pages change: a page is found at once, not by a look through all of them. */
function usePagesById(pages: readonly Page[]) {
  return useMemo(() => new Map(pages.map((page) => [page.id, page])), [pages]);
}

/**
 * A relation value in a card's sentence, as Notion's (seen 2026-11-22 at
 * 1440 dark): each page by its 18px icon 6 before its name in 500, the page
 * a run starts from in a blue tag with a 14px ↗, in the order picked, with
 * the language's list words between them; `small` as a line's field shows
 * them, 14px icons in 12px words.
 */
export function RelationValueChips({ items, property, small = false }: {
  items: readonly AutomationRelationItem[]; property: DbProperty; small?: boolean;
}) {
  const { t } = useTranslation("databaseView");
  const names = usePageNames();
  const byId = usePagesById(useRelationPages(property));
  const chips: ReactNode[] = items.map((item) => {
    if (typeof item !== "string") {
      return <span key={itemKey(item)} className={personStyles.source} data-small={small || undefined}>
        <RelationIcon size="tiny" aria-hidden="true" />{t("messageNames.triggerPage")}</span>;
    }
    const page = byId.get(item);
    return <span key={itemKey(item)} className={styles.page} data-small={small || undefined} data-automation-relation-page={item}>
      <PageIcon page={iconSource(page)} size={small ? 14 : 18} className={styles.pageIcon} />
      <span className={styles.pageName}>{page ? names.title(page) : t("untitled")}</span></span>;
  });
  if (small) return <span className={personStyles.smallChips}>{chips}</span>;
  return <ListedItems items={chips} />;
}

/**
 * A relation property's value, as Notion's window (seen 2026-11-22 at 1440
 * dark, 266 wide): a field holding the pages picked — each by its 14px icon
 * and name in 12px 500 with "×", the page a run starts from in a blue tag,
 * "⊗" taking all back — and "하나 이상의 페이지 검색"; under it "다음으로
 * 교체… ⌄" ("추가…", "제거…", "토글…"); then "↗ 페이지 실행" where the run
 * has a page in the database related to, and the related database's pages,
 * each by its 18px icon, a picked one ticked at its end and kept in the list.
 * A row picks or takes back; Enter picks the lit one, Backspace takes back the
 * last. A page line's (`line`) has no mode.
 */
export function AutomationRelationValues({ action, property, surface, line = false, onChange }: {
  action: EditPropertyAutomationAction; property: DbProperty; surface?: AutomationEditorSurface; line?: boolean;
  onChange(action: AutomationAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const names = usePageNames();
  const pages = useRelationPages(property, true);
  const [query, setQuery] = useState("");
  const [lit, setLit] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const items = useMemo(() => automationRelationItemsOf(action.value), [action.value]);
  const mode = automationValueModeOf(action.value);
  const latest = useRef({ action, items, mode });
  latest.current = { action, items, mode };
  const set = (next: AutomationRelationItem[], nextMode = latest.current.mode) => onChange({ ...latest.current.action,
    value: automationLiteralInMode(next, nextMode) });
  const chosenKeys = useMemo(() => new Set(items.map(itemKey)), [items]);
  const toggle = (item: AutomationRelationItem) => {
    const current = latest.current.items;
    set(current.some((candidate) => itemKey(candidate) === itemKey(item))
      ? current.filter((candidate) => itemKey(candidate) !== itemKey(item)) : [...current, item]);
    setQuery("");
    input.current?.focus({ preventScroll: true });
  };
  const byId = usePagesById(pages);
  const nameOf = useCallback((item: AutomationRelationItem) => {
    if (typeof item !== "string") return t("messageNames.triggerPage");
    const page = byId.get(item);
    return page ? names.title(page) : t("untitled");
  }, [byId, names, t]);
  const takesTriggerPage = relationTakesTriggerPage(property, surface);
  const words = query.trim();
  // Found as the relation picker finds them (relationPagesFound), the best
  // first, and named once: read again only as the pages, the words or the
  // names change, not as the highlight moves.
  const rows = useMemo(() => relationPagesFound<AutomationRelationItem>([
    ...(takesTriggerPage ? [TRIGGER_PAGE] : []),
    ...pages.map((page) => page.id),
  ], words, nameOf), [takesTriggerPage, pages, words, nameOf]);
  const rowNames = useMemo(() => rows.map(nameOf), [rows, nameOf]);
  const shownLit = Math.min(lit, Math.max(0, rows.length - 1));
  useEffect(() => { setLit(0); }, [words]);
  // A row's handlers are the same at each drawing, so a row the highlight leaves alone is not drawn again.
  const toggleNow = useRef(toggle);
  toggleNow.current = toggle;
  const pick = useCallback((item: AutomationRelationItem) => toggleNow.current(item), []);
  const hover = useCallback((index: number) => setLit(index), []);
  const keyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (rows.length) setLit((shownLit + (event.key === "ArrowDown" ? 1 : rows.length - 1)) % rows.length);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (rows[shownLit]) toggle(rows[shownLit]);
    } else if (event.key === "Backspace" && !query && items.length) {
      event.preventDefault();
      set(items.slice(0, -1));
    }
  };
  const token = (item: AutomationRelationItem) => {
    const name = nameOf(item);
    const remove = <button type="button" aria-label={t("removeRecipient", { name })} onClick={() => toggle(item)}>
      <X size="micro" aria-hidden="true" /></button>;
    if (typeof item !== "string") {
      return <span key={itemKey(item)} className={personStyles.fieldSource}><RelationIcon size="tiny" aria-hidden="true" />{name}{remove}</span>;
    }
    return <span key={itemKey(item)} className={`${personStyles.fieldPerson} ${styles.fieldPage}`}>
      <PageIcon page={iconSource(byId.get(item))} size={14} className={styles.pageIcon} />{name}{remove}</span>;
  };
  const listId = `automation-relation-${action.id}`;
  // Past 100 pages only those around what the list shows are drawn, as the
  // relation cell's picker draws them (usePickerWindow): each 28 high, 1 apart.
  const listRef = useRef<HTMLDivElement>(null);
  const drawn = usePickerWindow({ scrollRef: listRef, open: true, count: rows.length, keyAt: (index) => itemKey(rows[index]),
    sizeAt: () => 28, gap: 1, focused: shownLit });
  const drawRow = (line: PickerWindowLine) => {
    const item = rows[line.index];
    const row = <RelationRow key={line.key} item={item} index={line.index} listId={listId} lit={line.index === shownLit}
      chosen={chosenKeys.has(line.key)} name={rowNames[line.index] ?? ""} page={typeof item === "string" ? byId.get(item) : undefined}
      count={drawn.windowed ? rows.length : undefined} onHover={hover} onPick={pick} />;
    return line.top === undefined ? row : <div key={line.key} {...pickerWindowLine(line, drawn.measure)}>{row}</div>;
  };
  return <div className={`${personStyles.people} ${styles.value}`} data-automation-people={line ? "line" : ""} data-automation-relation-value="">
    <div className={`${personStyles.field} ${styles.field}`} onPointerDown={(event) => { if (event.target === event.currentTarget) event.preventDefault(); }}
      onClick={() => input.current?.focus()}>
      <span className={personStyles.tokens}>
        {items.map(token)}
        <input ref={input} className={personStyles.search} value={query} aria-label={t("automationTriggerSearchPages")} autoFocus={fieldFocusOnOpen()}
          placeholder={items.length ? undefined : t("automationTriggerSearchPages")} role="combobox" aria-expanded="true"
          aria-controls={listId} aria-activedescendant={rows[shownLit] ? `${listId}-${shownLit}` : undefined}
          onChange={(event) => setQuery(event.currentTarget.value)} onKeyDown={keyDown} />
      </span>
      {items.length ? <button type="button" className={personStyles.clear} aria-label={t("recipientsClear")} onClick={() => set([])}>
        <XCircleIcon size="small" aria-hidden="true" /></button> : null}
    </div>
    {line ? null : <div className={`${personStyles.modeRow} ${styles.modeRow}`}>
      <ValueModeButton mode={mode} onPick={(next) => set(latest.current.items, next)} />
    </div>}
    <div ref={listRef} id={listId} role="listbox" aria-multiselectable="true" aria-label={t("automationTriggerSearchPages")} className={personStyles.list}>
      {drawn.windowed
        ? <div ref={drawn.linesRef} role="presentation" style={pickerWindowBox(drawn.height)}>{drawn.lines.map(drawRow)}</div>
        : drawn.lines.map(drawRow)}
    </div>
  </div>;
}

/**
 * One page of the window's list: drawn again only when what it shows
 * changes — lit, picked, named — not each time the pointer lights another,
 * which drew all 5,000 of a database's pages again (2026-11-22: 161 ms a
 * row; redraw only what changed).
 */
const RelationRow = memo(function RelationRow({ item, index, listId, lit, chosen, name, page, count, onHover, onPick }: {
  item: AutomationRelationItem; index: number; listId: string; lit: boolean; chosen: boolean; name: string; page: Page | undefined;
  /** How many the list holds while it draws only some of them. */
  count?: number;
  onHover(index: number): void; onPick(item: AutomationRelationItem): void;
}) {
  return <div id={`${listId}-${index}`} role="option" aria-selected={chosen} className={personStyles.row} data-lit={lit || undefined}
    aria-setsize={count} aria-posinset={count === undefined ? undefined : index + 1}
    data-automation-relation={typeof item === "string" ? item : item.source}
    onPointerMove={() => { if (!lit) onHover(index); }}
    onPointerDown={(event) => event.preventDefault()} onClick={() => onPick(item)}>
    {typeof item === "string"
      ? <span className={styles.rowIcon}><PageIcon page={iconSource(page)} size={18} /></span>
      : <RelationIcon size="regular" aria-hidden="true" />}
    <span className={personStyles.name}>{name}</span>
    {chosen ? <CheckIcon size="small" aria-hidden="true" /> : null}
  </div>;
});
