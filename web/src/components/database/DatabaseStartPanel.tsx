import { Fragment, Suspense, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { StarterTemplateIcon } from "./DatabaseStarterPreview";
import { lazyScreen } from "@/lib/lazyScreen";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Session } from "@/types";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { databaseStarterLabel as label, databaseStarterMadeAtOnce, suggestedDatabaseStarters, type DatabaseStarterTemplate } from "@/lib/databaseStarterCatalog";
import { readDatabaseStarterUsed } from "@/databaseStarterApi";
import { placeAround, popoverPlacementStyle, popoverRectAt, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { keepLinkedViewWindowPlace } from "@/lib/linkedViewWindowPlace";
import { noteDatabaseStartPanelShown, noteLinkedFromStartPanel, takeStartPanelPageStep } from "@/lib/databaseStartPanel";
import { blockPeekedAlone, shownBlockElement } from "@/lib/shownBlockCopy";
import { keepLayerKeys, useOverlayLayer } from "@/lib/overlayLayer";
import { ArrowUpRightIcon, ChevronLeft, DotsHorizontal, ImportIcon, Plus, X } from "@/icons/leafen";
import { useProductPages } from "@/components/product/productPagesContext";
import { foundDatabaseSourcePlace } from "@/lib/pagePath";
import type { EditorDatabaseStartActions } from "@/components/editor/editorRuntimeTypes";
import { dataSourceDisplayTitle, inlineDatabasePlaceholderTitle } from "@/components/editor/databaseTitles";
import { DatabaseStarterCustomizer } from "./DatabaseStarterCustomizer";
import { bringNewDatabaseUp } from "./newDatabaseScroll";
import windowStyles from "@/components/sidebar/AddPageWindow.module.css";
import styles from "./DatabaseStartPanel.module.css";
import { useDockedPanelTop } from "./useDockedPanelTop";
import { DataSourceListRow } from "./DataSourceListRow";
import { holdingTyping, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { focusNextFrame } from "@/components/editor/focus";
import dockColumn from "./dockedPanelColumn.module.css";
import { inlineEndX, readsRightToLeft } from "@/lib/readingDirection";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneLinkSourceSheet } from "./DatabaseLinkPhoneSheets";
import { StartRowLit, sourceRowKey, useStartPanelRows } from "./startPanelRows";
import { EXISTING_COUNTED, existingSourcesShown, LINK_FIRST_SHOWN, LINK_MOST_SHOWN, useLinkableSources, useSourceListLimit } from "./linkableSources";
import { ExistingSources, StartSearchFound } from "./StartPanelSources";
import { SearchClearButton } from "@/components/PageActionSearch";
import { pagesByIdOf } from "@/lib/pageIndex";

const CsvImportDialog = lazyScreen(() => import("@/components/import/CsvImportDialog").then((module) => ({ default: module.CsvImportDialog })));

/**
 * The panel by a database just made from "/", as Notion's "새 데이터베이스"
 * (seen 2026-10-30): keep it empty, fill it from a CSV, start it from a
 * suggested template, or link an existing data source in its place. Escape,
 * a press elsewhere and "비어 있는 새 데이터 소스" keep the empty database.
 */
export function DatabaseStartPanel({ session, workspaceId, pageId, pageTitle, blockId, dataSourceId, viewId, addedSource, link = false, existing, relink = false, sources = false, docked: alone = false, chart = false, slash = false, onBlank, actions, onClose }: {
  session: Session; workspaceId: string; pageId: string; pageTitle: string; blockId: string; dataSourceId: string;
  /** Opened by a database just made from "/" on the page: put away, the caret goes to the page title's start. */
  slash?: boolean;
  /** The view it opened for: one waiting for its source, or back from its "새 보기", is linked alone (linkViewInPlaceOperations). */
  viewId?: string;
  /**
   * The database is opened alone: on its own page the panel hangs under the
   * toolbar row at the page's right, as its view settings do (Notion's, seen
   * 2026-11-04); in a side peek it floats at the window's top right.
   */
  docked?: boolean;
  /** A new chart lists the sources there are to chart in place of the suggestions, as Notion's (seen 2026-11-05). */
  chart?: boolean;
  /** A linked view waiting for its source ("데이터 소스 보기 연결"): the sources first (LinkList). */
  link?: boolean;
  /**
   * Such a view among others: Notion's "기존 데이터 소스 연결하기", the
   * sources the block links already first under "추천" (these ids), with no
   * empty source of its own (seen 2026-11-11 at 1440px and 402pt).
   */
  existing?: readonly string[];
  /** A linked view back from its "새 보기": a search over the templates and no empty source or CSV, as it has no source of its own (Notion's, seen 2026-11-05). */
  relink?: boolean;
  /** Its source picked in "기존 데이터 소스 연결하기", it opens on that list, its source first under "추천" (Notion's, seen 2026-11-09). */
  sources?: boolean;
  /** "비어 있는 새 데이터 소스" for such a view: it keeps its own empty source, named by a search where one was typed. */
  onBlank?(name?: string): void;
  /** Opened for a source just added ("새 데이터 소스", with Notion's search over the items). */
  addedSource: boolean;
  actions: EditorDatabaseStartActions; onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "editor", "common", "formView", "sidebar"]);
  // Opened alone in a side peek rather than on its own page, it floats
  // instead of hanging at the page's side (Notion's, seen 2026-11-09).
  const [peeked] = useState(() => alone && blockPeekedAlone(blockId));
  const docked = alone && !peeked;
  // A phone links a waiting view's source in Notion's whole sheet (PhoneLinkSourceSheet).
  const phoneLink = useSidebarLayout(true).phone && (link || relink);
  const [screen, setScreen] = useState<"start" | "link" | "csv">(relink && sources ? "link" : "start");
  // Put away without a source or template — Esc, "×" or a press elsewhere,
  // on its first screen or on "기존 데이터 소스 연결하기" — the panel of a
  // database made from "/" in a page's body leaves the caret at that page
  // title's start; on a database's own page "비어 있는 새 데이터 소스" puts it
  // in the empty database's title and Esc or "×" leaves none. Notion's,
  // measured 2026-11-15 in Chrome at 1440, dark, with no script in the page
  // at any time: the screen after the action and one letter typed — "z" went
  // in before the page title's first letter after Esc, "×" and "비어 있는 새
  // 데이터 소스" on an inline database's panel, after Esc and "×" on a linked
  // view's chooser, after Esc and "×" on "기존 데이터 소스 연결하기" reached
  // from the panel and after a press on the page's margin — in a database
  // row's page opened whole too, the row's title taking it (seen 2026-11-18
  // in Chrome at 1440 with Notion's AI on, a row made by hand, two tabs at
  // different times, the title out of sight in one); on a full-page
  // database's it named the database after "비어 있는 새 데이터 소스" and
  // went nowhere after Esc or "×", nor after "새 '〈search〉' 데이터 소스",
  // which names it by the search (seen 2026-11-17 in Chrome at 1440 on a
  // guest with no AI, the one shown that search). Seen in Chrome only: the Notion app took no keys from the
  // background. A press on the sidebar is one on the margin: its search, pressed, only put the panel away, the
  // letter going before the title's first letter (seen 2026-11-21 in Chrome at 1440, "/표 보기"'s panel, two
  // tabs). A press whose target still took the focus keeps it. The page stays put (focusTitleStartInPlace).
  const putAway = (blank = false, pressed = false) => {
    onClose();
    if (docked) { if (blank) requestPageTitleFocus(pageId); }
    // A frame on, once the panel is gone; a letter typed meanwhile waits for it (focusNextFrame).
    else if (slash) focusNextFrame(() => ({ titleStart: pageId }), () => pressed && Boolean(document.activeElement)
      && document.activeElement !== document.body && !holdingTyping());
  };
  const startBlank = () => putAway(true);
  const [template, setTemplate] = useState<DatabaseStarterTemplate | null>(null);
  const [busy, setBusy] = useState(false), [failed, setFailed] = useState(false);
  const [filter, setFilter] = useState(""), filterRef = useRef<HTMLInputElement>(null);
  // A chart just made opens on its rows, unlit until ↓, which go through
  // none past their ends, as the same window's for a waiting view (LinkList,
  // Notion's seen 2026-11-14 at 1440). Notion's first field is its AI's,
  // focused, wherever its AI is offered (seen 2026-11-15 to 2026-11-17 in
  // Chrome and the Notion app, inline and on a database's own page), which
  // Leafen leaves out with its built-in agent. A new database, inline or
  // opened alone (on its own page or in a side peek), starts as Notion's does
  // where its AI is not offered, with the search "데이터 소스 추가 또는
  // 연결하기…" focused (seen 2026-11-17 on a guest with no AI, on a
  // database's own page and inline, each in the Notion app and twice in
  // Chrome at 1440, dark: one letter typed after it opened went into it; a
  // floating window's search band is the window's own ground, as Notion's
  // inline one looks), its rows lit only once ↓ or ↑ moved among them, each
  // list a search then shows lighting its first (StartSearchFound), "입력 값
  // 지우기" bringing back the first screen.
  const fresh = !addedSource && !relink, searchFirst = fresh && !chart && !link;
  const panelRef = useRef<HTMLDivElement>(null), firstRef = useRef<HTMLButtonElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const startListRef = useRef<HTMLDivElement>(null), startRows = useStartPanelRows(startListRef, "throughNone");
  const row = (key: string) => ({ "data-key": key, "data-active": startRows.lit === key ? "true" : undefined });
  const [query, setQuery] = useState(""), named = query.trim();
  const keyed = useRef(false), lightFound = useRef<string | null>(null);
  const search = (next: string) => {
    setQuery(next);
    lightFound.current = keyed.current && next.trim() ? next : null;
    startRows.light(keyed.current && !next.trim() ? "blank" : null);
  };
  const work = useRef<AbortController | null>(null);
  useEffect(() => () => work.current?.abort(), []);
  const shown = (text: string) => !filter.trim() || text.toLocaleLowerCase().includes(filter.trim().toLocaleLowerCase());
  // Notion's suggestions, inline or on a database's own page alike (seen
  // 2026-11-05 in a tab block and 2026-11-06 on a page), those the page
  // already shows last (suggestedDatabaseStarters); none until that is read,
  // so the list never changes under the pointer. "템플릿 더 보기" is left out
  // with Notion's gallery.
  const [used, setUsed] = useState<ReadonlySet<string> | null>(null);
  useEffect(() => {
    if (chart || link) return;
    const controller = new AbortController();
    readDatabaseStarterUsed(session, pageId, controller.signal).then(({ templates }) => setUsed(new Set(templates)))
      .catch(() => { if (!controller.signal.aborted) setUsed(new Set()); });
    return () => controller.abort();
  }, [session, pageId, chart, link]);
  const suggested = used ? suggestedDatabaseStarters(used, (candidate) => shown(label(candidate.name))) : [];
  const only = addedSource ? dataSourceId : undefined;
  // Opened with its database low in the window, the page first brings it up
  // (bringNewDatabaseUp). Opened again by a linked view's "←", its database
  // already stands where it is seen and the page stays put (Notion's, seen
  // 2026-11-07). Before the panel is first placed, so it never shows where the
  // database stood.
  useLayoutEffect(() => {
    if (docked || relink) return;
    const host = shownBlockElement(blockId);
    return host ? bringNewDatabaseUp(host) : undefined;
    // Once, as it opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const place = usePopoverPlacement(panelRef, phoneLink ? null : (size, viewport) => {
    // Where the new database's view settings open, as Notion's (seen
    // 2026-11-02): 3px under its "새로 만들기", from 6px past its toolbar's
    // end, no nearer than 12px to the window's right edge — over neither its
    // toolbar nor its table. By its top right while its toolbar isn't drawn
    // yet, and at the window's top if the database isn't either.
    // Placed again as the block draws what it waits on (its tab row, its toolbar).
    // In a side peek it stands at the window's top right corner, 12px in,
    // over the peek's bar (Notion's, seen 2026-11-09 for a waiting link).
    // All of it mirrors where the screen reads from the right: past the
    // toolbar's left, 12px from the window's left. `from` is the panel's
    // left; it starts (placeAround's "start") at its right there.
    const rtl = readsRightToLeft(), from = (left: number) => rtl ? left + size.width : left;
    if (peeked) return placeAround(popoverRectAt({ top: 12, bottom: 12, left: from(rtl ? 12 : viewport.width - 12 - size.width) }), size, viewport,
      { align: "start", gap: 0, margin: 12, whole: true });
    const host = shownBlockElement(blockId), contextElement = host ?? undefined;
    // A view still waiting for its source has its tab row and no toolbar:
    // the panel opens where its settings would, 3px under that row's
    // buttons (28px, 6px down) from 6px past its right end less 8 (seen
    // 2026-11-02: 470 under a block at 433), or 2px over the row where only
    // there it shows whole; with room on neither side, on the roomier one
    // moved into the window, never cut (seen 2026-11-08 in a 723 window:
    // 232..575 over a row at 577, 367..710 under one at 335, 12..355 over one at 345).
    // It keeps its side as a search shortens or lengthens its list, its foot
    // on the row over it or its top under it (seen 2026-11-08: 224..567 to
    // 299..567 and 429..567 over a row at 569, 207..550 to 207..567 under one at 170).
    const pendingTabs = popoverRectOf(host?.querySelector("[data-database-link-pending-tabs]"));
    if (pendingTabs) {
      // Short of room it slides in to 12px from the window's edge, as Notion's (seen 2026-11-05), not back over the table.
      const left = rtl ? Math.max(pendingTabs.left + 8 - 6 - size.width, 12) : Math.min(pendingTabs.right - 8 + 6, viewport.width - 12 - size.width);
      return placeAround(popoverRectAt({ top: pendingTabs.top + 1, bottom: pendingTabs.top + 34, left: from(left), contextElement }), size, viewport,
        { align: "start", gap: 3, margin: 12, whole: true, roomier: true, holdSide: true });
    }
    // Never cut: 2px over its toolbar row (its buttons stand 6px down) where
    // only there it shows whole, else moved up into the window whole over it
    // (seen 2026-11-08 in a 696 window: 33..590 over a row at 592, 126..683
    // over one at 547 or 315).
    const toolbar = popoverRectOf(host?.querySelector("[data-database-toolbar]"));
    const button = popoverRectOf(host?.querySelector("[data-database-new-row]"));
    if (toolbar && button) {
      const left = rtl ? Math.max(toolbar.left - 6 - size.width, 12) : Math.min(toolbar.right + 6, viewport.width - 12 - size.width);
      return placeAround(popoverRectAt({ top: button.top - 5, bottom: button.bottom, left: from(left), contextElement }), size, viewport,
        { align: "start", gap: 3, margin: 12, whole: true });
    }
    const block = popoverRectOf(host);
    const anchor = block ? popoverRectAt({ top: block.top, bottom: block.top, left: inlineEndX(block, 0, host), contextElement })
      : popoverRectAt({ top: 80, bottom: 80, left: rtl ? 16 : viewport.width - 16 });
    return placeAround(anchor, size, viewport, { align: "end" });
  }, [blockId, screen], undefined, {
    kind: "picker", restoreFocus: false, onEscape: () => { if (!work.current) putAway(); },
    // A press anywhere else keeps the empty database too, inline or on its own
    // page (Notion's, seen 2026-11-06: its title, a line above), and does
    // nothing more: while it shows, the page under it takes no pointer (the
    // page shield, as floating menus do), its database's tab row showing no
    // "+" and its blocks no handle under the pointer (Notion's view settings
    // panel lays a clear cover over the window, seen 2026-11-14 at 1440: a
    // press on "+ 새 페이지" or the page's title only closed it).
    onOutsidePress: () => { if (!work.current) putAway(false, true); },
    // Its database's "⚙", pressed while it shows, puts it away itself (noteDatabaseStartPanelShown).
    inside: () => [shownBlockElement(blockId)?.querySelector("[data-database-settings-trigger]")],
    initialFocus: screen === "link" || link || searchFirst ? searchRef : fresh ? panelRef : filterRef,
  });
  // Its database's "⚙" stands pressed while it shows, as it is that button's window (Notion's, seen 2026-11-13).
  const closeRef = useRef(onClose); closeRef.current = onClose;
  useLayoutEffect(() => noteDatabaseStartPanelShown(blockId, () => { if (!work.current) closeRef.current(); }), [blockId]);
  // Its linked view's "새 보기" opens where it stands (linkedViewWindowPlace).
  useEffect(() => { if ((link || relink) && !docked) keepLinkedViewWindowPlace(panelRef.current, place); }, [link, relink, docked, place]);
  // A view waiting for its source hangs it under its tab row, which runs to
  // the page column's end, as Notion's (seen 2026-11-09: the panel 128 down
  // under a row at 88, ending with it).
  const dockRef = useMemo(() => ({
    get current() { return shownBlockElement(blockId)?.querySelector<HTMLElement>("[data-database-toolbar], [data-database-link-pending-tabs]") ?? null; },
  }), [blockId]);
  const dockTop = useDockedPanelTop(dockRef, docked, panelRef);
  async function apply(run: (signal: AbortSignal) => Promise<boolean>) {
    if (work.current) return;
    const controller = new AbortController(); work.current = controller;
    setBusy(true); setFailed(false);
    try { if (await run(controller.signal)) onClose(); else if (!controller.signal.aborted) setFailed(true); }
    catch { if (!controller.signal.aborted) setFailed(true); }
    finally { if (work.current === controller) { work.current = null; setBusy(false); } }
  }
  const linkSource = (source: DatabaseContainerSource) => actions.linkSource(blockId, source, only, viewId);
  // A suggestion with nothing to choose is made at once (databaseStarterMadeAtOnce).
  const pick = (candidate: DatabaseStarterTemplate) => {
    const selection = databaseStarterMadeAtOnce(candidate);
    if (selection) void apply((signal) => actions.applyTemplate(blockId, selection, signal, only));
    else setTemplate(candidate);
  };
  if (phoneLink) {
    return <PhoneLinkSourceSheet busy={busy} failed={failed} load={actions.loadDatabaseSources} own={relink ? "" : dataSourceId} blank={!relink}
      existing={link ? existing : undefined}
      onBlank={(name) => { onBlank?.(name); onClose(); }} onClose={onClose}
      onChoose={(source) => { noteLinkedFromStartPanel(blockId, relink ? "sources" : "start"); void apply(() => linkSource(source)); }} />;
  }
  if (screen === "csv") {
    return <Suspense fallback={null}><CsvImportDialog session={session} workspaceId={workspaceId} page={{ pageId, title: pageTitle }}
      merge mergeDataSourceId={dataSourceId} onImported={onClose} onClose={onClose} /></Suspense>;
  }
  if (template) {
    return <StarterWindow label={t("sidebar:addWindow.customize", { title: label(template.name) })}
      onClose={() => { if (!work.current) setTemplate(null); }}>
      <DatabaseStarterCustomizer key={template.key} template={template} busy={busy} failed={failed} previewAuthor={actions.previewAuthor} fromPanel
        onCancel={() => { setTemplate(null); setFailed(false); }}
        onApply={(selection) => void apply((signal) => actions.applyTemplate(blockId, selection, signal, only))} />
    </StarterWindow>;
  }
  const heading = addedSource ? t("newDataSource") : inlineDatabasePlaceholderTitle();
  const existingOnly = link && existing !== undefined;
  const rowsTakeKeys = fresh && !searchFirst && !link && screen === "start"; // ↓ ↑ Enter for its rows, with no search
  return createPortal(<div ref={panelRef} role="dialog" aria-label={screen === "link" || existingOnly ? t("linkExistingDataSource") : heading}
    aria-busy={busy || undefined} className={`${styles.panel} ${docked ? dockColumn.column : ""}`}
    style={docked ? { top: `max(var(--page-panel-top, var(--topbar-height)), ${dockTop ?? 0}px)` } : { position: "fixed", ...popoverPlacementStyle(place) }}
    tabIndex={rowsTakeKeys ? -1 : undefined} data-database-start-panel={screen} data-docked={docked || undefined}
    onKeyDown={(event) => {
      if (screen === "start" && !link && !work.current && takeStartPanelPageStep(event, pageId)) return;
      if (rowsTakeKeys && event.target === event.currentTarget) startRows.onKeyDown(event);
      keepLayerKeys(event);
    }}>
    {existingOnly ? <SourceList searchRef={searchRef} busy={busy} failed={failed} load={actions.loadDatabaseSources} own={dataSourceId}
      suggest={existing} onClose={() => putAway()}
      onChoose={(source) => { keepLinkedViewWindowPlace(panelRef.current, place); noteLinkedFromStartPanel(blockId, "start"); void apply(() => linkSource(source)); }} />
    : link ? <LinkList searchRef={searchRef} busy={busy} failed={failed} load={actions.loadDatabaseSources} own={dataSourceId}
      onBlank={(name) => { onBlank?.(name); putAway(); }} onClose={() => putAway()}
      onChoose={(source) => { keepLinkedViewWindowPlace(panelRef.current, place); noteLinkedFromStartPanel(blockId, "start"); void apply(() => linkSource(source)); }} />
    : screen === "start" ? <>
      <header className={styles.head}><strong>{heading}</strong>
        <button type="button" className={styles.close} aria-label={t("common:actions.close")} onClick={() => putAway()}><X size="tiny" /></button></header>
      {searchFirst ? <span className={styles.searchBox}>
        <input ref={searchRef} className={styles.search} value={query} placeholder={t("startPanel.linkAddOrConnect")} aria-label={t("startPanel.linkAddOrConnect")}
          disabled={busy} data-filled={query ? "" : undefined} onChange={(event) => search(event.target.value)} onKeyDown={(event) => {
            if (event.key === "ArrowDown" || event.key === "ArrowUp") keyed.current = true;
            startRows.onKeyDown(event);
          }} />
        <SearchClearButton value={query} inputRef={searchRef} over onClear={() => search("")} /></span>
      : fresh ? null : <input ref={filterRef} className={styles.search} value={filter} placeholder={t(relink ? "startPanel.linkSearch" : "startPanel.search")} aria-label={t(relink ? "startPanel.linkSearch" : "startPanel.search")}
        onKeyDown={startRows.onKeyDown} onChange={(event) => setFilter(event.target.value)} />}
      <div ref={startListRef} className={styles.rows} onPointerMove={startRows.onPointerMove}>
      <StartRowLit.Provider value={startRows}>
      {searchFirst && named ? <StartSearchFound query={query} name={named} busy={busy} failed={failed} load={actions.loadDatabaseSources} own={dataSourceId} lit={startRows.lit}
        onShown={(first, read) => { if (lightFound.current === read) { lightFound.current = null; startRows.light(first ?? "named"); } }}
        onChoose={(source) => { noteLinkedFromStartPanel(blockId, "start"); void apply(() => linkSource(source)); }}
        onNamed={() => { onBlank?.(named); putAway(); }} onCsv={() => setScreen("csv")} />
      : <>
      {!relink && shown(t("startPanel.blank")) ? <button ref={firstRef} type="button" className={`${styles.item} ${styles.start} ${styles.blank}`} {...row("blank")} onClick={startBlank}><span className={styles.mark} aria-hidden="true"><Plus size="regular" /></span>{t("startPanel.blank")}</button> : null}
      {!relink && shown(t("startPanel.csv")) ? <button type="button" className={`${styles.item} ${styles.start}`} {...row("csv")} onClick={() => setScreen("csv")}><ImportIcon size="regular" aria-hidden="true" />{t("startPanel.csv")}</button> : null}
      {chart ? <ExistingSources query={filter} first={LINK_FIRST_SHOWN} most={LINK_MOST_SHOWN} busy={busy} failed={failed} load={actions.loadDatabaseSources} own={dataSourceId}
        onChoose={(source) => { noteLinkedFromStartPanel(blockId, "start"); void apply(() => linkSource(source)); }} /> : null}
      {!chart && suggested.length ? <div className={`${styles.heading} ${styles.listHeading}`}>{t("startPanel.suggested")}</div> : null}
      {!chart && suggested.map((candidate) => <button key={candidate.key} type="button" className={`${styles.item} ${styles.start}`} data-starter-template={candidate.key}
        {...row(`template:${candidate.key}`)} onClick={() => pick(candidate)}><span className={styles.mark} data-tone={candidate.tone} aria-hidden="true"><StarterTemplateIcon icon={candidate.icon} size={16} /></span>{label(candidate.name)}</button>)}
      <hr className={styles.rule} />
      <button type="button" className={`${styles.item} ${styles.small} ${styles.start}`} {...row("link")} onClick={() => setScreen("link")}>
        <ArrowUpRightIcon size="regular" aria-hidden="true" />{t("linkExistingDataSource")}</button>
      </>}
      </StartRowLit.Provider>
      </div>
    </> : <SourceList searchRef={searchRef} busy={busy} failed={failed} load={actions.loadDatabaseSources} own={relink ? "" : dataSourceId}
      ownCounted suggest={relink ? [dataSourceId] : undefined}
      onBack={() => { setScreen("start"); setFailed(false); }} onClose={() => putAway()}
      onChoose={(source) => {
        if (relink) keepLinkedViewWindowPlace(panelRef.current, place);
        noteLinkedFromStartPanel(blockId, "sources"); void apply(() => linkSource(source));
      }} />}
  </div>, document.body);
}

/**
 * The linked source chooser, as Notion's: a search, then each source by its
 * place, a 20px cylinder for its database as in every list of sources to
 * link (Notion's "collection", seen 2026-11-11), five of the hundred it
 * reads before "N개 더 보기", a search's first twenty, the reach every
 * source window keeps (useSourceListLimit, existingSourcesShown). "기존 데이터
 * 소스" stays when it holds none, "결과 없음" under it, searched in vain or
 * with every source found under "추천" (Notion's, seen 2026-11-11).
 */
export function SourceList({ searchRef, busy, failed, load, own, ownCounted = false, suggest = [], onBack, onClose, onChoose }: {
  searchRef: React.RefObject<HTMLInputElement | null>; busy: boolean; failed: boolean; own: string;
  /**
   * Whether the database's own empty source, not offered, counts first among
   * the five and the hundred: a new database's, as Notion's (seen 2026-11-18
   * twice from its panel: 4 and "95개 더 보기", pressed 24 and "75개 더 보기").
   */
  ownCounted?: boolean;
  /** The sources a linked view shows, first under "추천" (Notion's, seen 2026-11-05). */
  suggest?: readonly string[];
  /** "←" back to the panel's first screen, where it was reached from there. */
  onBack?(): void;
  load: EditorDatabaseStartActions["loadDatabaseSources"]; onClose(): void; onChoose(source: DatabaseContainerSource): void;
}) {
  const { t } = useTranslation(["databaseView", "editor", "common", "formView"]);
  // Five, a search's first twenty, twenty more a press, the reach kept through searches (useSourceListLimit; Notion's,
  // seen 2026-11-18 twice: 20 + "53개 더 보기", 40 + 33, another search 40, emptied 39 + "60개 더 보기").
  const [query, setQuery] = useState(""), { limit, more: showMore } = useSourceListLimit(query);
  const search = setQuery;
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  // The new database itself isn't offered: it would link to its own empty source.
  const { sources, cursor, loading, failed: loadFailed, more, shownFor } = useLinkableSources(load, query, own, EXISTING_COUNTED, suggest);
  // Reached from the panel's first screen, it takes the keys at once, as
  // Notion's "연결하기…" (measured 2026-11-15 in Chrome at 1440 with no script
  // in the page: after "기존 데이터 소스 연결하기" a typed "z" went into its
  // search).
  useEffect(() => { searchRef.current?.focus({ preventScroll: true }); }, [searchRef]);
  // None lit until ↓ or a search, which lights the first found as it comes
  // in (Notion's, seen 2026-11-05); round past its ends, "N개 더 보기"
  // among its rows (seen 2026-11-14).
  const listRef = useRef<HTMLDivElement>(null), rows = useStartPanelRows(listRef, "round");
  const lightFound = useRef<string | null>(null);
  const suggested = sources.filter((source) => suggest.includes(source.dataSourceId));
  const others = sources.filter((source) => !suggest.includes(source.dataSourceId));
  const ordered = [...suggested, ...others];
  const { shown: visible, hidden } = existingSourcesShown(sources.map(({ dataSourceId }) => dataSourceId), ordered,
    { searched: query.trim() !== "", limit, ahead: ownCounted && own ? 1 : 0, key: ({ dataSourceId }) => dataSourceId });
  const groupsShown = [{ heading: t("startPanel.suggested"), items: suggested.filter((source) => visible.includes(source)) },
    { heading: t("editor:starter.sources"), items: others.filter((source) => visible.includes(source)) }]
    .filter(({ items }, index) => index > 0 || items.length > 0);
  const listed = groupsShown.flatMap(({ items }) => items);
  useEffect(() => {
    if (lightFound.current === null || shownFor !== lightFound.current) return;
    lightFound.current = null;
    rows.light(listed[0] ? sourceRowKey(listed[0]) : null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shownFor, sources]);
  return <>
    <header className={styles.head}>
      {onBack ? <button type="button" className={styles.back} aria-label={t("common:actions.back")} onClick={onBack}><ChevronLeft size="small" /></button> : null}
      <strong>{t("linkExistingDataSource")}</strong>
      <button type="button" className={styles.close} aria-label={t("common:actions.close")} onClick={onClose}><X size="tiny" /></button></header>
    {/* ↑ ↓ move the lit row and Enter presses it, the keys staying in the search, as Notion's (seen 2026-11-05). */}
    <input ref={searchRef} className={styles.search} value={query} placeholder={t("startPanel.linkSearch")} aria-label={t("startPanel.linkSearch")}
      disabled={busy} onChange={(event) => { search(event.target.value); lightFound.current = event.target.value; }} onKeyDown={rows.onKeyDown} />
    <div ref={listRef} className={styles.rows} onPointerMove={rows.onPointerMove}>
    {groupsShown.map(({ heading, items }, index) => <Fragment key={heading}>
        <div className={`${styles.heading} ${styles.listHeading}`}>{heading}</div>
        {/* Under its name, where it sits, as Notion's path line (DataSourceListRow). */}
        {items.map((source) => <DataSourceListRow key={`${source.id}:${source.dataSourceId}`} className={styles.source}
          disabled={busy} data-start-source={source.dataSourceId} data-key={sourceRowKey(source)}
          data-active={rows.lit === sourceRowKey(source) ? "true" : undefined} onClick={() => onChoose(source)}
          source={source} name={dataSourceDisplayTitle(source.title, source.sourceTitles?.length ?? 1)} place={foundDatabaseSourcePlace(source, pagesById)} />)}
        {index > 0 && !loading && !others.length ? <p className={styles.note}>{t("noSearchResults")}</p> : null}
      </Fragment>)}
    {hidden > 0 ? <button type="button" className={`${styles.item} ${styles.more}`} disabled={busy}
      data-key="more" data-active={rows.lit === "more" ? "true" : undefined} onClick={showMore}>
      <DotsHorizontal size="regular" aria-hidden="true" />{t("showMoreDataSources", { count: hidden })}</button> : null}
    {loading ? <p className={styles.note} role="status">{t("editor:starter.loading")}</p> : null}
    {loadFailed || failed ? <p className={styles.note} role="alert">{t("editor:starter.loadFailed")}</p> : null}
    {cursor && !loading ? <button type="button" className={`${styles.item} ${styles.small}`} disabled={busy}
      data-key="load-more" data-active={rows.lit === "load-more" ? "true" : undefined} onClick={more}>{t("formView:loadMore")}</button> : null}
    </div>
  </>;
}

/**
 * A waiting linked view's panel, as Notion's "새 데이터베이스" from "데이터
 * 소스 보기 연결" (seen 2026-11-02): its search "데이터 소스 추가 또는
 * 연결하기…" focused, "+ 비어 있는 새 데이터 소스", then "기존 데이터
 * 소스", each by where it sits, three before "자세히 표시", which shows five.
 * Searching, the empty source's row goes and one named by the search follows
 * the sources found, under a rule, alone where none is found (Notion's "새
 * '〈search〉' 데이터 소스", seen 2026-11-08; its Korean shows "{name}" for
 * the search, which is drawn here). It keeps the database's own empty source,
 * named by the search.
 */
function LinkList({ searchRef, busy, failed, load, own, onBlank, onClose, onChoose }: {
  searchRef: React.RefObject<HTMLInputElement | null>; busy: boolean; failed: boolean; own: string;
  load: EditorDatabaseStartActions["loadDatabaseSources"]; onBlank(name?: string): void; onClose(): void; onChoose(source: DatabaseContainerSource): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [query, setQuery] = useState("");
  const name = query.trim();
  // Its rows wait unlit until ↓ (the empty source's first) and go through
  // none past their ends; a search lights the new source it names, and
  // emptied, the first source as it comes in (Notion's, seen 2026-11-14).
  const listRef = useRef<HTMLDivElement>(null), rows = useStartPanelRows(listRef, "throughNone");
  const lightFound = useRef<string | null>(null);
  // The empty source's row is a start row, its "+" in a 32px tile (Notion's, seen 2026-11-09).
  const blank = (label: string, named?: string) => <button type="button" className={`${styles.item} ${styles.start} ${styles.blank}`} disabled={busy} onClick={() => onBlank(named)}
    data-start-link-blank={named === undefined ? "" : "named"} data-key={named === undefined ? "blank" : "named"}
    data-active={rows.lit === (named === undefined ? "blank" : "named") ? "true" : undefined}>
    <span className={styles.mark} aria-hidden="true"><Plus size="small" /></span><span className={styles.blankName}>{label}</span></button>;
  return <>
    <header className={styles.head}>
      <strong>{inlineDatabasePlaceholderTitle()}</strong>
      <button type="button" className={styles.close} aria-label={t("common:actions.close")} onClick={onClose}><X size="tiny" /></button></header>
    <input ref={searchRef} className={styles.search} value={query} placeholder={t("startPanel.linkAddOrConnect")} aria-label={t("startPanel.linkAddOrConnect")}
      disabled={busy} onKeyDown={rows.onKeyDown} onChange={(event) => {
        const next = event.target.value;
        setQuery(next);
        if (next.trim()) { lightFound.current = null; rows.light("named"); } else { lightFound.current = next; rows.light(null); }
      }} />
    <div ref={listRef} className={styles.rows} onPointerMove={rows.onPointerMove}>
    <StartRowLit.Provider value={rows}>
    {name ? null : <><hr className={styles.rule} />{blank(t("startPanel.blank"))}</>}
    <ExistingSources query={query} first={LINK_FIRST_SHOWN} most={LINK_MOST_SHOWN} busy={busy} failed={failed} load={load} own={own} onChoose={onChoose}
      onShown={(first, read) => { if (lightFound.current === read) { lightFound.current = null; rows.light(first); } }}
      foot={name ? (found) => <>{found ? <hr className={styles.rule} /> : null}{blank(t("startPanel.blankNamed", { name }), name)}</> : undefined} />
    </StartRowLit.Provider>
    </div>
  </>;
}

/** A suggestion's customize step, in the add window's frame. */
function StarterWindow({ label: ariaLabel, onClose, children }: { label: string; onClose(): void; children: React.ReactNode }) {
  const { t } = useTranslation("common");
  const windowRef = useRef<HTMLElement>(null), overlayRef = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: windowRef, modalOwned: () => [overlayRef.current], onEscape: onClose });
  return createPortal(<div ref={overlayRef} className={windowStyles.overlay} style={{ zIndex }} onKeyDown={keepLayerKeys}>
    <button className={windowStyles.backdrop} type="button" tabIndex={-1} aria-label={t("actions.close")} onClick={onClose} />
    <section ref={windowRef} role="dialog" aria-modal="true" aria-label={ariaLabel} className={windowStyles.window} data-database-start-template>{children}</section>
  </div>, document.body);
}
