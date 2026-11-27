import { memo, useRef, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DbProperty, Page } from "@/lib/types";
import { PageIconGlyph } from "../page/PageIcon";
import { CaretRightFill, DragHandleIcon, Plus } from "../icons";
import { CardValueTip } from "./CardValueTip";
import { CardValuePress } from "./CardValuePress";
import { fromWithinCard } from "./CardPropertySlots";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { PropertyDateCell } from "./PropertyDateCell";
import { keepLayerKeys } from "@/lib/overlayLayer";
import type { listCompletionBinding } from "./listViewModel";
import { conditionalColorBackground } from "./conditionalColorModel";
import type { ColorName } from "./colors";
import styles from "./databaseSheet";
import { CardRowTitle } from "./CardRowTitle";
import { RowCommentCount } from "./RowCommentCount";
import { PeekHalo } from "./PeekHalo";
import { CardChoiceHalo } from "./cardSelection";
import { useRowDisplayTitle } from "./untitledRowName";
import { valueAsRead } from "./valueAsRead";
import { AddHereTip, HandleTip, IconTipButton } from "../IconTipButton";
import { optionKeyName } from "@/lib/keyboardShortcuts";
import { statusAfterCheckboxClick, statusCheckboxState } from "./propertyStatusPolicy";
import { readsOtherRows } from "./otherRowsRead";
import type { ListRowOpenMode } from "./ListView";
import type { useDragReorder } from "@/lib/useDragReorder";

// A sub-item's step in, and the start of a row's icon after the toggle's room (Notion's list, seen 2026-10-11).
export const SUBITEM_INDENT = 24, ROW_ICON_START = 35;

/** What every row of a list draws alike, the same object while none of it changes. */
export type ListRowFrame = {
  /** What the view lists before its title, and the rest at the row's end (listRowSides). */
  leading: readonly DbProperty[];
  others: readonly DbProperty[];
  environment: PropertyCellEnvironment | undefined;
  ownValue: ((rowId: string, propertyId: string) => ReactNode) | null | undefined;
  readOnly: boolean;
  adds: boolean;
  /** Rows move by their handles (a view not read only, its archived rows not shown). */
  moves: boolean;
  expandable: boolean;
  completion: ReturnType<typeof listCompletionBinding>;
  showIcons: boolean;
  /** "페이지 이름 직접 편집": a press on the row, not its values, types its title (titleCaretAt). */
  directEdit: boolean;
  titled: boolean;
  contextIdentity: string;
  flattened: boolean;
  hiddenSubtasks: boolean;
};

/** What a row's presses do, by the list as last drawn (the same object for as long as the list is drawn). */
export type ListRowWays = {
  openRow(rowId: string, mode?: ListRowOpenMode): void;
  openClick(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  openKey(rowId: string, event: ReactKeyboardEvent<HTMLElement>): void;
  contextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  handleMenu(rowId: string, element: HTMLElement): void;
  handle(rowId: string): ReturnType<ReturnType<typeof useDragReorder>["handle"]>;
  /** Makes a row under it (over it, `before`), in its line's group (`line`, viewGroupLines.ts). */
  addNear(row: Page, before: boolean, line?: string): void;
  toggle(rowId: string, loads: boolean): void;
  loadMore(rowId: string): void;
  setCompletion(rowId: string, propertyId: string, value: string): void;
  datePressed(): void;
  commitDate(rowId: string, propertyId: string, value: unknown): void;
  /** Its title typed in place on one of its lines (`line`, viewGroupLines.ts), the caret where `caret` says (titleCaretAt) or at its end. */
  editTitle(line: string, caret?: number): void;
  editingChange(rowId: string, editing: boolean): void;
  choose(rowId: string): void;
  commitTitle(rowId: string, value: string): Promise<unknown>;
};

type ListViewRowProps = {
  row: Page;
  index: number;
  depth: number;
  hasChildren: boolean;
  childCount: number;
  expanded: boolean;
  /** Its sub-items' next page: none, one to load, or one loading. */
  more: "none" | "ready" | "loading";
  parentTitle: string;
  tint: ColorName | undefined;
  dropSide: string | undefined;
  dragging: string | undefined;
  faded: boolean;
  editNow: boolean;
  /** Where the caret goes in its title as it starts being typed (titleCaretAt). */
  editCaret: number | undefined;
  added: boolean;
  locked: boolean;
  frame: ListRowFrame;
  context: PropertyValueReadContext;
  ways: ListRowWays;
  rowRef: (element: HTMLElement | null) => void;
  /** Its line, which its handle drags and the view chooses: under its group, a row under several groups having one in each (viewLinesDrag). */
  line?: string;
};

/**
 * One row of a list, drawn again only when what it shows changed: a row made,
 * a title edited or the list's own state changing drew every row of it again,
 * some 2,300 parts for 30 rows, three times inside one "+ 새 페이지" press
 * (2026-11-14, NAS + CPU ×2). The values it shows are read from the list's
 * value context, made anew as any row comes or goes; a row whose values read
 * only its own row (no relation or rollup, nor a formula naming one —
 * otherRowsRead.ts) is not drawn again for it (sameListRow).
 */
export const ListViewRow = memo(function ListViewRow({ row, index, depth, hasChildren, childCount, expanded, more, parentTitle, tint, dropSide, dragging,
  faded, editNow, editCaret, added, locked, frame, context, ways, rowRef, line }: ListViewRowProps) {
  const { t } = useTranslation(["listView", "tableView"]);
  const rowTitle = useRowDisplayTitle();
  const { readOnly, completion } = frame;
  const listValue = (property: DbProperty) => added && property.type === "date" && !row.properties?.[property.id] ? (
    <span key={property.id} className={styles.listAddDate} onPointerDown={() => ways.datePressed()}
      onClick={(event) => event.stopPropagation()} onKeyDown={keepLayerKeys}>
      <PropertyDateCell prop={property} value={null} rowId={row.id} inline emptyLabel={t("listView:addDate", { name: property.name })}
        onCommit={(value) => ways.commitDate(row.id, property.id, value)} />
    </span>
  ) : (
    <CardValueTip key={property.id} name={property.name}><CardValuePress className={styles.listField} property={property} page={row}
      environment={readOnly || locked ? undefined : frame.environment}>
      {frame.ownValue?.(row.id, property.id) ?? <PropValue context={context} row={row} prop={property} interactive={false}
        onOpenPage={(pageId) => ways.openRow(pageId)} />}
    </CardValuePress></CardValueTip>
  );
  const isStructuralPlaceholder = row.__structuralPlaceholder === true;
  const title = isStructuralPlaceholder ? t("tableView:restrictedParent") : rowTitle(row);
  const statusValue = completion ? valueAsRead(row, completion.property) : null;
  // Checked when finished, a dash while in progress, as a status shown as a checkbox (Notion's "내 작업", seen 2026-11-08).
  const checkState = completion ? statusCheckboxState(completion.property.config, statusValue) : "unchecked";
  const completed = checkState === "checked";
  const subitemLabel = expanded ? t("tableView:collapseSubitems", { title }) : t("tableView:expandSubitems", { title });
  // A row moves among those under the same parent, by the handle its pointer shows (Notion's, seen 2026-11-05).
  const moves = frame.moves && !isStructuralPlaceholder;
  return (
    <div ref={rowRef} data-index={index} className={styles.listTreeRow} data-list-row-id={row.id}
      data-drop-side={moves ? dropSide : undefined} data-dragging={moves ? dragging : undefined}
      data-subitem-context={faded ? "true" : undefined}
      data-list-row-title={title}
      data-conditional-color={tint}
      style={tint ? { background: conditionalColorBackground(tint) } : undefined}
      data-structural-placeholder={isStructuralPlaceholder ? "true" : undefined}
      data-subitem-depth={depth > 0 ? depth : undefined}>
      {moves ? <span className={styles.listRowTools} data-list-row-tools>
        {/* Its "+" adds a row under it, or over it with Option held, as a table row's (Notion's, seen 2026-11-08), the two marks a table row's 20px,
            named and tipped as a block's (TableRowHandles). */}
        {frame.adds ? <IconTipButton wait="handle" label={t("blockHandle:buttons.addBlockBelow", { key: optionKeyName() })} tip={<AddHereTip />} placement="bottom"
          onClick={(event) => ways.addNear(row, event.altKey, line)}>
          <Plus size="regular" aria-hidden="true" />
        </IconTipButton> : null}
        <IconTipButton tabIndex={-1} wait="handle" label={t("blockHandle:buttons.dragHint")} tip={<HandleTip drag />} placement="bottom"
          data-row-menu-handle data-card-handle-for={line ?? row.id} {...ways.handle(line ?? row.id)} onClick={(event) => ways.handleMenu(row.id, event.currentTarget)}>
          <DragHandleIcon size="regular" aria-hidden="true" />
        </IconTipButton>
      </span> : null}
      {depth > 0 && <span className={styles.subitemIndent} style={{ width: depth * SUBITEM_INDENT }} aria-hidden="true" />}
      {/* With sub-items on, every row keeps a toggle's room, as Notion's list (seen 2026-10-11): a row with
          sub-items shows its toggle, one without shows it under the pointer, to open it and add one. */}
      {frame.expandable && (hasChildren || (!readOnly && !isStructuralPlaceholder) ? (
        <button type="button" className={styles.subitemToggle} data-visible={hasChildren || expanded ? "true" : undefined}
          aria-expanded={expanded} aria-label={subitemLabel} title={subitemLabel}
          onClick={() => ways.toggle(row.id, hasChildren)}>
          <CaretRightFill aria-hidden="true" />
        </button>
      ) : <span className={styles.subitemToggle} aria-hidden="true" />)}
      {expanded && more !== "none" && (
        <button type="button" className={styles.subitemToggle} data-subitem-load-more
          data-visible="true" aria-label={`${t("tableView:loadMore")}: ${title}`}
          title={`${t("tableView:loadMore")}: ${title}`} disabled={more === "loading"}
          onClick={() => ways.loadMore(row.id)}>
          <Plus size="tiny" aria-hidden="true" />
        </button>
      )}
      {!isStructuralPlaceholder && completion && (
        <label className={styles.listCompletion} data-readonly={readOnly || locked ? "true" : undefined}>
          <input type="checkbox" ref={(box) => { if (box) box.indeterminate = checkState === "mixed"; }}
            aria-label={t(completed ? "listView:markIncomplete" : "listView:markComplete", { title })}
            checked={completed} disabled={readOnly || locked}
            onChange={() => {
              // An unfinished one finishes; a finished one goes back, as its table's checkbox does.
              const next = statusAfterCheckboxClick(completion.property.config, statusValue);
              if (next) ways.setCompletion(row.id, completion.property.id, next);
            }} />
        </label>
      )}
      {isStructuralPlaceholder ? (
        <div className={styles.listRow} aria-label={title}>
          <span className={styles.listTitle}>{title}</span>
        </div>
      ) : (
        // What its values' pickers, drawn over the page, send here through React isn't the row's (CardValuePress).
        <div className={styles.listRow} role="button" tabIndex={0} data-row-card data-card-choice={line ?? row.id} data-card-title={title}
          aria-label={t("listView:openRow", { title })}
          onClick={(e) => {
            if (!fromWithinCard(e)) return;
            // "페이지 이름 직접 편집": a plain press on the row, not on its values, types its title (titleCaretAt) on the line pressed.
            if (frame.directEdit && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey
              && !(e.target instanceof Element && e.target.closest("[data-list-values]"))) ways.editTitle(line ?? row.id, titleCaretAt(e.currentTarget, e.clientX, e.clientY));
            else ways.openClick(row.id, e);
          }}
          onKeyDown={(e) => { if (fromWithinCard(e)) ways.openKey(row.id, e); }}
          onAuxClick={(e) => { if (e.button === 1 && fromWithinCard(e)) { e.preventDefault(); ways.openRow(row.id, "new_tab"); } }}
          onContextMenu={(e) => { if (fromWithinCard(e)) ways.contextMenu(row.id, e); }}>
          <PeekHalo itemId={row.id} />
          <CardChoiceHalo itemId={line ?? row.id} />
          {frame.leading.length > 0 && <span className={styles.listLead} data-list-values>{frame.leading.map(listValue)}</span>}
          {frame.showIcons && (
            <span className={styles.listIcon}>
              <PageIconGlyph page={{
                icon: row.icon ?? "",
                iconType: row.iconType,
                workspaceId: row.workspaceId,
                kind: row.kind,
                notionIcon: row.notionIcon,
                hasContent: row.hasContent,
              }} size={18} />
            </span>
          )}
          <CardRowTitle key={frame.contextIdentity} row={row} inline className={styles.listTitle}
            // Asked once: let go once its typing ends, not as it starts, which drew the list again in the press.
            editNow={editNow} editCaret={editCaret} opensBeside={frame.directEdit} onEditingChange={editNow ? (editing) => ways.editingChange(row.id, editing) : undefined}
            onEscaped={added ? () => ways.choose(row.id) : undefined}
            onOpenSide={() => ways.openRow(row.id, "side")}
            readOnly={readOnly || !frame.titled} onCommit={(value) => ways.commitTitle(row.id, value)}
            after={<RowCommentCount rowId={row.id} />} />
          {frame.flattened && row.subitemParentId && (
            <span className={styles.subitemParentIndicator} data-subitem-parent-id={row.subitemParentId}
              title={t("tableView:subitemOf", { title: parentTitle })}>
              {t("tableView:subitemOf", { title: parentTitle })}
            </span>
          )}
          {frame.hiddenSubtasks && childCount > 0 && (
            <span className={styles.subitemCount} data-subitem-count={childCount}>
              {t("tableView:subitemCount", { count: childCount })}
            </span>
          )}
          <span className={styles.listMeta} data-list-values>{frame.others.map(listValue)}</span>
        </div>
      )}
    </div>
  );
}, sameListRow);

/**
 * Where a press on a row puts the caret in its title, as Notion's list with
 * "페이지 이름 직접 편집" on (seen 2026-11-15 at 1440 in Chrome, judged by a
 * letter typed after the press with no script before it): on its words,
 * between the letters pressed, "한날 22" pressed between 한 and 날 typing
 * "한Z날 22"; past them on the row, at their end. Undefined is the end.
 */
function titleCaretAt(row: Element, x: number, y: number): number | undefined {
  const words = row.querySelector("[data-row-title] [data-search-text]");
  const at = words ? caretRangeAt(x, y) : null;
  if (!words || !at || !words.contains(at.startContainer)) return undefined;
  const before = document.createRange();
  before.selectNodeContents(words);
  before.setEnd(at.startContainer, at.startOffset);
  return before.toString().length;
}

/** The collapsed range a point on screen falls in, by the browser's own caret placement. */
function caretRangeAt(x: number, y: number): Range | null {
  const position = (document as Document & { caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null })
    .caretPositionFromPoint?.(x, y);
  if (!position) return document.caretRangeFromPoint?.(x, y) ?? null;
  const range = document.createRange();
  range.setStart(position.offsetNode, position.offset);
  return range;
}

/**
 * The list's frame for its rows, the same object while what it holds reads
 * the same: its lists by their items, its status binding by its parts (both
 * made anew as the view's settings change — a row made in its own order).
 */
export function useSteadyFrame(next: ListRowFrame): ListRowFrame {
  const kept = useRef(next);
  const was = kept.current;
  const same = (Object.keys(next) as (keyof ListRowFrame)[]).every((key) => {
    const left: unknown = was[key], right: unknown = next[key];
    if (left === right) return true;
    if (Array.isArray(left) && Array.isArray(right)) return left.length === right.length && left.every((item, index) => item === right[index]);
    return key === "completion" && left !== null && right !== null && typeof left === "object" && typeof right === "object"
      && Object.keys(right).length === Object.keys(left).length
      && Object.entries(right).every(([part, value]) => (left as Record<string, unknown>)[part] === value);
  });
  if (!same) kept.current = next;
  return kept.current;
}

/**
 * A list's cell ways as they are now, the same object for as long as the list
 * is drawn: a row not drawn again presses and edits its values with the
 * snapshot, context and operations the list was last drawn with, as a
 * table's cells do (liveCellWays).
 */
export function liveEnvironment(latest: { readonly current: PropertyCellEnvironment | undefined }): PropertyCellEnvironment {
  const now = () => (latest.current ?? {}) as object;
  return new Proxy({} as PropertyCellEnvironment, {
    get: (_, key) => Reflect.get(now(), key),
    has: (_, key) => Reflect.has(now(), key),
    ownKeys: () => Reflect.ownKeys(now()),
    getOwnPropertyDescriptor: (_, key) => {
      const found = Reflect.getOwnPropertyDescriptor(now(), key);
      return found && { ...found, configurable: true };
    },
  });
}

// The lookups of other rows in a list's value context, made anew as any row comes or goes.
const OTHER_ROWS_LOOKUPS = new Set(["pagesById", "hydratedRelationTargetIds", "snapshotIdentity", "propsByDatabaseId"]);

/**
 * A row is drawn again when what it shows changed: any of its parts but the
 * value context, which counts only where a value it shows reads other rows
 * (a relation, a rollup, a formula naming one) or its database's properties
 * changed.
 */
function sameListRow(previous: ListViewRowProps, next: ListViewRowProps) {
  for (const key of Object.keys(next) as (keyof ListViewRowProps)[]) {
    if (key !== "context" && previous[key] !== next[key]) return false;
  }
  const before = previous.context, after = next.context;
  if (before === after) return true;
  for (const key of Object.keys(after) as (keyof PropertyValueReadContext)[]) {
    if (!OTHER_ROWS_LOOKUPS.has(key) && before[key] !== after[key]) return false;
  }
  return [...next.frame.leading, ...next.frame.others].every((property) => {
    const properties = after.propsByDatabaseId[property.databaseId];
    return before.propsByDatabaseId[property.databaseId] === properties && !readsOtherRows(property, properties);
  });
}
