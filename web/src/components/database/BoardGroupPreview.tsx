import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { hasExplicitPageIcon, PageIconGlyph, type PageIconSource } from "@/components/page/PageIcon";
import { SidebarPagePicker } from "@/components/sidebar/SidebarPagePicker";
import { useProductPages } from "@/components/product/productPagesContext";
import { useDataSourceName } from "./useDataSourceName";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { ArrowUpRightIcon, CenterPeekIcon, DatabaseCylinderIcon, DotsHorizontal, FilterIcon, Plus, Search, X } from "@/icons/leafen";
import { pageHref } from "@/lib/navigation";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { pageNamesNow } from "@/lib/pageNames";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute, replaceRoute } from "@/lib/router";
import { createSavedViewPage, showSavedViewPageHere } from "@/lib/savedViewPage";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import type { DbProperty, DbView, Page, ViewConfig, ViewFilter } from "@/lib/types";
import { boardGroupPreviewView } from "./boardGroupPreviewModel";
import { chipStyle } from "./colors";
import { statusGroupTitle } from "./StatusGroupLabel";
import type { StatusStage } from "./propertyStatusPolicy";
import { DatabaseQuickFilterBar } from "./DatabaseQuickFilterBar";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { PropertyIcon } from "./PropertyIcon";
import { applyView, type QueryBounds } from "./query";
import styles from "./BoardGroupPreview.module.css";
import { useRowDisplayTitle } from "./untitledRowName";

type PreviewGroup = { key: string; label: string; color?: string; statusStage?: StatusStage; statusOption?: boolean; rows: readonly Page[] };

/** What the preview's filter bar starts from and narrows. */
export type BoardGroupPreviewFilters = {
  /** The board's view, whose filters and sorts the preview starts with. */
  view: GraphView;
  graphProperties: readonly GraphProperty[];
  snapshot: DatabaseFilterSnapshot;
  /** The board's sub-item option's filter, shown and changed only there. */
  lockedFilter: ViewFilter | null;
  /** The group's own rule, or none for a group of ranges. */
  rule: (key: string) => ViewFilter | null;
  /** Every row the board shows, in its order, which the group's rule narrows. */
  rows: readonly Page[];
  pagesById: Readonly<Record<string, Page>>;
  bounds: QueryBounds;
  currentUserId?: string;
  /** Where a view saved from it links: the database's own container. */
  save?: { containerId: string; dataSourceId: string };
  /** The group's rule shows as a filter of its own, as a chart's (boardGroupPreviewModel.ts). */
  ruleAsPill?: boolean;
};

/** A part of the preview's rows, which it shows under its chip as a group of its own. */
export type BoardGroupPreviewSection = { key: string; label: string; color: string; rows: readonly Page[] };

/** A row's own words a search finds: its title and its text values. */
function rowText(row: Page, properties: readonly DbProperty[]) {
  const texts = [pageNamesNow.title(row)];
  for (const property of properties) {
    const value = row.properties?.[property.id];
    if (typeof value === "string") texts.push(value);
  }
  return texts.join("\n").toLocaleLowerCase();
}

/**
 * A pinned group's rows in a table, over the board, as Notion's "표 데이터
 * 미리보기" (seen 2026-10-11): 800×496 with 12px corners, the group's chip,
 * then "검색", "⋯" and "닫기"; a filter bar — "규칙 1개", the group's rule
 * among the board's own, the sub-item option's dimmed pill and "+ 필터" —
 * whose changes stay in the preview; a table of every property of its rows —
 * the title, the group's own property, then the others in the database's
 * order, each head with its property's icon, each title with its page's icon
 * and, under the pointer, "열기", which opens the row in the middle over the
 * window (Notion's, seen 2026-11-12) — or "필터 편집하기" when none is left.
 * "⋯" saves it as a view on a chosen page, or opens its database.
 * A chart's group opens it too, under the group's name in bold and its rows
 * by what the chart stacks by, each part under its chip (Notion's, seen
 * 2026-11-11).
 */
export function BoardGroupPreview({ group, properties, groupPropertyId, context, databaseTitle, databaseSourceId, databaseIcon, databaseHref, filters, plainTitle = false,
  sections, onOpenRow, onClose }: {
  group: PreviewGroup;
  properties: readonly DbProperty[];
  /** The property the group is of, its column right after the title's. */
  groupPropertyId?: string;
  context: PropertyValueReadContext;
  databaseTitle: string;
  /** The source its rows are of, which names it in its "⋯" (useDataSourceName). */
  databaseSourceId: string;
  /** Its database's own icon, the cylinder where it has none. */
  databaseIcon?: PageIconSource;
  /** Where its database opens, when it can. */
  databaseHref?: string | null;
  filters?: BoardGroupPreviewFilters;
  /** The group's name in bold, not its chip, as a chart's. */
  plainTitle?: boolean;
  /** Its rows in parts, each under its chip. */
  sections?(rows: readonly Page[]): BoardGroupPreviewSection[];
  /** Opens a row in the middle of the screen, the window left under it. */
  onOpenRow(rowId: string): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "common", "boardView", "tableView"]);
  const rowTitle = useRowDisplayTitle();
  const sourceName = useDataSourceName({ id: databaseSourceId, title: databaseTitle });
  const toast = useOptionalProductToast();
  const product = useProductPages();
  // The open page's graph, whose source a saved view's page shows at once.
  const pageGraph = useOpenPageGraphStore();
  const backdropRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [more, setMore] = useState<PopoverRect | null>(null);
  const [picking, setPicking] = useState<DOMRect | null>(null);
  const [saving, setSaving] = useState(false);
  const [editRequest, setEditRequest] = useState(0);
  const rule = filters?.rule(group.key) ?? null;
  // The preview's own filters, which leave the board's view as it is.
  const [local, setLocal] = useState(() => filters ? boardGroupPreviewView(filters.view, rule, filters.ruleAsPill) : null);
  const [folded, setFolded] = useState<ReadonlySet<string>>(new Set());
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: backdropRef, modalOwned: () => [backdropRef.current], restoreFocus: true,
    onEscape: () => { if (more) setMore(null); else onClose(); },
  });
  useEffect(() => { if (searching) searchRef.current?.focus(); }, [searching]);
  // A row opened from it shows in the middle over it, the window dimmed under the row's
  // until the row closes (Notion's, seen 2026-11-12): it goes under the peek's 50 till then.
  const [underRow, setUnderRow] = useState(false);
  useEffect(() => {
    if (!underRow) return;
    let seen = false;
    const check = () => {
      if (document.querySelector("[data-page-peek]")) seen = true;
      else if (seen) setUnderRow(false);
    };
    const observer = new MutationObserver(check);
    observer.observe(document.body, { childList: true, subtree: true });
    check();
    const giveUp = window.setTimeout(() => { if (!seen) setUnderRow(false); }, 3000);
    return () => { observer.disconnect(); window.clearTimeout(giveUp); };
  }, [underRow]);
  const openRow = (rowId: string) => { setUnderRow(true); onOpenRow(rowId); };
  const rank = (property: DbProperty) => property.type === "title" ? 0 : property.id === groupPropertyId ? 1 : 2;
  const ordered = [...properties].sort((a, b) => rank(a) - rank(b));
  const needle = query.trim().toLocaleLowerCase();
  // Its rows stand in the order of the view it opened from, which keeps its own (viewRowOrder.ts): a chart's
  // newest first, as Notion's window (seen 2026-11-12: F, B, A for rows made A to F through the API).
  const narrowed = filters && local ? applyView([...(rule ? filters.rows : group.rows)], [...properties], {
    id: local.id, databaseId: local.dataSourceId, name: local.name, type: "table", position: 0,
    config: { ...local.config, rowOrder: filters.view.config.rowOrder, sorts: undefined, search: undefined,
      quickFilters: [...(local.config.quickFilters as ViewConfig["quickFilters"] ?? []), ...(filters.lockedFilter ? [filters.lockedFilter] : [])] } as ViewConfig,
  } satisfies DbView, { ...filters.pagesById }, { currentUserId: filters.currentUserId }, filters.bounds) : group.rows;
  const rows = needle ? narrowed.filter((row) => rowText(row, properties).includes(needle)) : narrowed;
  const parts = sections?.(rows).filter((part) => part.rows.length > 0);
  const table = (shown: readonly Page[]) => (
    <table className={styles.table}>
      <thead>
        <tr>{ordered.map((property) => <th key={property.id} scope="col" data-title={property.type === "title" ? "true" : undefined}>
          <span className={styles.columnHead}><PropertyIcon property={property} size="small" />{property.name}</span>
        </th>)}</tr>
      </thead>
      <tbody>
        {shown.map((row) => (
          <tr key={row.id} data-board-group-preview-row={row.id}>
            {ordered.map((property) => property.type === "title" ? (
              <td key={property.id} data-title="true">
                <span className={styles.title}>
                  <span className={styles.titleIcon} aria-hidden="true"><PageIconGlyph page={{ icon: row.icon ?? "", iconType: row.iconType, workspaceId: row.workspaceId,
                    kind: row.kind, notionIcon: row.notionIcon, hasContent: row.hasContent }} size={16} /></span>
                  <span className={styles.titleText}>{rowTitle(row)}</span>
                </span>
                <button type="button" className={styles.openRow} aria-label={t("databaseView:peekModes.center")} onClick={() => openRow(row.id)}
                  data-board-group-preview-open-row={row.id}>
                  <CenterPeekIcon size="regular" aria-hidden="true" /><span>{t("tableView:openRowLabel")}</span>
                </button>
              </td>
            ) : (
              <td key={property.id}><PropValue context={context} row={row} prop={property} interactive={false} /></td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
  const session = product?.session, workspaceId = product?.workspaceId;
  const canSave = Boolean(filters?.save && local && session && workspaceId);
  const save = async (pageId: string) => {
    if (!filters?.save || !local || !session || !workspaceId || saving) return;
    setPicking(null);
    const source = { ...filters.save, title: databaseTitle };
    // The page shows and opens on the next frame where this device can make it (savedViewPage.ts).
    const from = `${window.location.pathname}${window.location.search}`;
    const shown = product && showSavedViewPageHere(session, {
      workspaceId, parentPageId: pageId, config: local.config, source, outbox: product.mutationOutbox, pages: product.pages,
      shown: pageGraph?.get() ?? null, onPageChange: (change) => product.setData((current) => ({ ...current, pages: change(current.pages) })),
      refresh: () => product.refreshPages?.() ?? Promise.resolve(),
      announce: (message, tone) => { if (tone && toast) toast.notify(message, tone); else product.onStatus?.(message, tone); },
      refused: () => { if (shown && window.location.pathname === pageHref(shown.id)) replaceRoute(from); },
    });
    if (shown) {
      onClose();
      pushRoute(pageHref(shown.id));
      return;
    }
    setSaving(true);
    try {
      const page = await createSavedViewPage(session, { workspaceId, parentPageId: pageId, config: local.config, source });
      // The new page opens once the page list knows it.
      await product?.refreshPages?.()?.catch(() => undefined);
      onClose();
      pushRoute(pageHref(page.id));
    } catch {
      toast?.notify(t("databaseView:couldntSaveView"), "error");
    } finally {
      setSaving(false);
    }
  };
  const dialog = (
    // The backdrop click is a pointer shortcut; "닫기" and Escape close it too.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div ref={backdropRef} className={styles.backdrop} style={{ zIndex: underRow ? 49 : zIndex }} tabIndex={-1}
      onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <div className={styles.dialog} role="dialog" aria-modal="true" aria-label={t("databaseView:groupPreview")} data-board-group-preview={group.key}>
        <div className={styles.head} data-plain-title={plainTitle ? "" : undefined}>
          <span className={plainTitle ? styles.headTitle : styles.group}>
            {plainTitle ? group.label : statusGroupTitle(group, group.label)
              ?? (group.color ? <span className={styles.chip} style={chipStyle(group.color)}>{group.label}</span> : <span>{group.label}</span>)}
          </span>
          {searching ? (
            <input ref={searchRef} className={styles.search} value={query} placeholder={t("databaseView:previewSearch")}
              aria-label={t("databaseView:previewSearch")} onChange={(event) => setQuery(event.target.value)}
              onBlur={() => { if (!query) setSearching(false); }} data-board-group-preview-search />
          ) : null}
          <span className={styles.tools}>
            <button type="button" className={styles.tool} aria-label={t("common:actions.search")} onClick={() => setSearching(true)}>
              <Search size="small" aria-hidden="true" />
            </button>
            {databaseHref || canSave ? (
              <button type="button" className={styles.tool} aria-label={t("databaseView:previewMore")} aria-haspopup="menu" disabled={saving}
                aria-expanded={Boolean(more)} onClick={(event) => setMore(more ? null : popoverRectOf(event.currentTarget))}>
                <DotsHorizontal size="small" aria-hidden="true" />
              </button>
            ) : null}
            <button type="button" className={styles.close} aria-label={t("common:actions.close")} onClick={onClose}>
              <X size="tiny" aria-hidden="true" />
            </button>
          </span>
        </div>
        {filters && local ? (
          <div className={styles.filters} data-board-group-preview-filters>
            <DatabaseQuickFilterBar view={local} properties={filters.graphProperties} snapshot={filters.snapshot} writable
              advancedRequest={editRequest} addLabel={t("databaseView:filter")} onUpdateView={setLocal} />
          </div>
        ) : null}
        <div className={styles.scroller}>
          {parts ? parts.map((part) => (
            <section key={part.key} className={styles.section} data-board-group-preview-section={part.key}>
              <button type="button" className={styles.sectionHead} aria-expanded={!folded.has(part.key)}
                onClick={() => setFolded((current) => { const next = new Set(current); if (!next.delete(part.key)) next.add(part.key); return next; })}>
                <span className={styles.sectionToggle} data-folded={folded.has(part.key) ? "" : undefined} aria-hidden="true">▼</span>
                <span className={styles.chip} style={chipStyle(part.color)}>{part.label}</span>
              </button>
              {folded.has(part.key) ? null : table(part.rows)}
            </section>
          )) : table(rows)}
          {rows.length === 0 && filters && !needle ? (
            <div className={styles.empty} data-board-group-preview-empty>
              <button type="button" className={styles.editFilters} onClick={() => setEditRequest((count) => count + 1)}>
                <FilterIcon size="micro" aria-hidden="true" />{t("boardView:editFilters")}
              </button>
            </div>
          ) : null}
        </div>
      </div>
      {more ? (
        // An untitled source is "새 데이터베이스" there, one of several "새 데이터 소스", as Notion's (seen 2026-11-11, 2026-11-18).
        <PreviewMoreMenu anchor={more} title={sourceName} icon={databaseIcon}
          saveLabel={canSave ? t("databaseView:previewSaveAsView") : undefined}
          onSave={() => { const anchor = more; setMore(null); setPicking(new DOMRect(anchor.left, anchor.top, anchor.right - anchor.left, anchor.bottom - anchor.top)); }}
          onOpen={databaseHref ? () => { setMore(null); onClose(); pushRoute(databaseHref); } : undefined}
          onClose={() => setMore(null)} />
      ) : null}
      {picking && session && workspaceId ? (
        <SidebarPagePicker anchor={picking} placeholder={t("databaseView:previewSavePlaceholder")} pages={product?.pages ?? []}
          session={session} workspaceId={workspaceId} foundDatabase="cylinder" onChoose={(pageId) => { if (pageId) void save(pageId); }}
          onClose={() => setPicking(null)} />
      ) : null}
    </div>
  );
  return createPortal(dialog, document.body);
}

/**
 * Its "⋯", as Notion's (seen 2026-11-11): as wide as its rows, 180 to 250px,
 * right under the button, its middle under the button's; saving it as a view on a page, then "↗ ⛁ 〈DB〉
 * 열기" — the database's icon, the cylinder where it has none, 4px in, its
 * name 2px on, the words of the language's order after it.
 */
function PreviewMoreMenu({ anchor, title, icon, saveLabel, onSave, onOpen, onClose }: {
  anchor: PopoverRect; title: string; icon?: PageIconSource; saveLabel?: string; onSave(): void; onOpen?: () => void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => ({ ...placeAround(anchor, size, viewport, { gap: 0 }), placement: "bottom" }), [anchor], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose, initialFocus: "first" });
  const database = (
    <span className={styles.menuDatabase}>
      <span className={styles.menuDatabaseIcon} aria-hidden="true">
        {icon && hasExplicitPageIcon(icon) ? <PageIconGlyph page={icon} size={16} /> : <DatabaseCylinderIcon pixels={16} />}
      </span>
      <span className={styles.menuDatabaseName}>{title}</span>
    </span>
  );
  return (
    <div ref={ref} className={styles.menu} role="menu" style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
      {saveLabel ? (
        <button type="button" role="menuitem" className={styles.menuItem} onClick={onSave} data-board-group-preview-save>
          <Plus size="regular" aria-hidden="true" />
          <span className={styles.menuName}>{saveLabel}</span>
        </button>
      ) : null}
      {onOpen ? (
        <button type="button" role="menuitem" className={styles.menuItem} onClick={onOpen} data-board-group-preview-open>
          <ArrowUpRightIcon size="regular" aria-hidden="true" />
          <span className={styles.menuOpen}>
            <Trans t={t} i18nKey="previewOpenDatabase" values={{ name: title }} components={{ db: database }} />
          </span>
        </button>
      ) : null}
    </div>
  );
}
