import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { readDatabaseStarterSourceViews } from "@/databaseStarterApi";
import type { Session } from "@/types";
import { placeAround, popoverPlacementStyle, popoverRectAt, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { keepLinkedViewWindowPlace, linkedViewWindowPlace, placeAtLinkedViewWindow } from "@/lib/linkedViewWindowPlace";
import { keepLayerKeys } from "@/lib/overlayLayer";
import { ArrowLeft, Plus, X } from "@/icons/leafen";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewIcon, viewDisplayName } from "./databaseViewShared";
import { beginLinkedViewMaking, endLinkedViewMaking, leftLinkedView } from "@/lib/databaseStarterLinked";
import { viewTakenAfter } from "@/lib/viewTakenAfter";
import { markSourceChosenView } from "@/lib/viewTabRequests";
import { useDockedPanelTop } from "./useDockedPanelTop";
import dockColumn from "./dockedPanelColumn.module.css";
import styles from "./DatabaseLinkedStarter.module.css";
import { inlineEndX } from "@/lib/readingDirection";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneLinkedViewSheet } from "./DatabaseLinkPhoneSheets";

// The source's views shown before "N개 더 보기", as Notion's (seen 2026-11-02).
const SHOWN_VIEWS = 7;

/**
 * A source card first creates the linked page; the page then chooses its
 * view, as Notion's "새 보기" (seen 2026-11-02 and 11-03 at 1280px): where
 * the view settings open, a search when the source has more than seven
 * views, "+ 새 표 보기" ("+ 새 보드 보기" for a board), then its views under
 * its name, seven and "N개 더 보기". A view chosen is copied — its name as
 * typed, its filters and sorts saved with it, its own order of rows
 * (viewTakenAfter, the same copy a new view's "〈소스〉의 조회수" makes) — and its settings
 * open with the keys in that name (focusPickedViewName). "+ 새 표 보기" leaves it the layout it waited as,
 * unnamed, its rows the newest first (linkedStarterViewConfig), and goes on
 * to the new view window every new view has, in this window's place
 * (DatabaseViewSettingsPanel; Notion's, seen 2026-11-18 in Chrome 1440 in
 * two tabs: "새 보기 ×" with the view's icon and its name focused, the
 * layouts, "데이터베이스 ↗ 〈원본〉 ›" and "완료", which Enter in the name
 * presses). Left unchosen, it stays that unnamed layout. Right after
 * its source panel it stands where that stood (linkedViewWindowPlace); on
 * its own page it hangs at the page's side as the view settings do (Notion's,
 * seen 2026-11-09). It asks only where the view was linked, once
 * (makingLinkedView): left open as its page goes, the view is left unchosen.
 */
export function DatabaseLinkedStarter({ session, view, sourceTitle, toolbarRef, alone = false, onCommit, onBack }: {
  session: Session; view: GraphView; sourceTitle: string;
  toolbarRef: RefObject<HTMLDivElement | null>;
  /** The view it ends as, and what opens after: a new view's window ("new", by "+ 새 표 보기"), or its settings after one of its source's views ("picked"). */
  onCommit(view: GraphView, settings: false | "new" | "picked"): Promise<unknown>;
  /** The database is opened alone, not on a phone: on its own page the window hangs under its toolbar row; in a side peek it floats. */
  alone?: boolean;
  /**
   * "←": back to the start panel's screen its source was picked in, where
   * the block stays linked until another is picked (Notion's, seen
   * 2026-11-05 and 11-09, inline or on its own page).
   */
  onBack?(): void;
}) {
  const { t } = useTranslation(["editor", "databaseView", "common", "formView"]);
  const labels = databaseViewLabels();
  // A phone chooses in Notion's whole sheet "새 보기" (PhoneLinkedViewSheet), a choice going on to the view's settings sheet.
  const phone = useSidebarLayout(true).phone;
  const [views, setViews] = useState<GraphView[]>([]), [cursor, setCursor] = useState<string>();
  const [loading, setLoading] = useState(true), [failed, setFailed] = useState(false), [busy, setBusy] = useState(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const loader = useRef<AbortController | null>(null), working = useRef(false);
  const retryCursor = useRef<string | undefined>(undefined), menuRef = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState(""), [allShown, setAllShown] = useState(false);
  const source = String(view.config.starterSourceContainerId ?? "");
  const [from] = useState(() => linkedViewWindowPlace());
  // Opened alone in a side peek, it floats where its source panel stood.
  const [peeked, setPeeked] = useState(false);
  useLayoutEffect(() => { setPeeked(Boolean(toolbarRef.current?.closest("[data-inline-database-peek]"))); }, [toolbarRef]);
  const docked = alone && !peeked;
  const dockTop = useDockedPanelTop(toolbarRef, docked, menuRef);
  // Gone without a choice as its page is left, the view is left unchosen
  // where it is next drawn here (takeLeftLinkedView); drawn again on the same
  // page, it asks on.
  const chosen = useRef(false);
  useEffect(() => {
    beginLinkedViewMaking(view.id);
    return () => endLinkedViewMaking(view.id, chosen.current);
  }, [view.id]);
  // Closed, or gone back from, it leaves the view unchosen (leftLinkedView).
  const leave = () => { if (!working.current) void commit(leftLinkedView(view), false); };
  const back = onBack ? () => { void commit(leftLinkedView(view), false).then(onBack); } : undefined;
  const place = usePopoverPlacement(menuRef, phone ? null : (size, viewport) => {
    if (from) return placeAtLinkedViewWindow(from, size, viewport);
    // Where its view settings open: 3px under its "새로 만들기", from 6px past the
    // toolbar's end, no nearer than 12px to the window's right (seen 2026-11-05);
    // past its left where the screen reads from the right.
    const toolbar = popoverRectOf(toolbarRef.current)!;
    const button = popoverRectOf(toolbarRef.current?.querySelector("[data-database-new-row]") ?? toolbarRef.current)!;
    return placeAround(popoverRectAt({ top: button.top, bottom: button.bottom, left: inlineEndX(toolbar, 6, toolbarRef.current) }), size, viewport, { align: "start", gap: 3, margin: 12 });
  }, [view.id], undefined, {
    kind: "picker", restoreFocus: false, onEscape: leave,
    // A press anywhere else closes it as "×" does, floating or hung (Notion's, seen 2026-11-09).
    onOutsidePress: leave,
    initialFocus: searchRef,
  });
  async function load(after?: string) {
    loader.current?.abort(); const controller = new AbortController(); loader.current = controller;
    setLoading(true); setFailed(false); retryCursor.current = after;
    try {
      const response = await readDatabaseStarterSourceViews(session, source, view.dataSourceId, after, controller.signal);
      if (controller.signal.aborted || loader.current !== controller) return;
      if (after && response.nextCursor === after) throw new Error("Invalid continuation");
      setViews(current => [...new Map([...(after ? current : []), ...response.views].map(v => [v.id, v])).values()]);
      setCursor(response.nextCursor);
    } catch { if (!controller.signal.aborted) setFailed(true); }
    finally { if (loader.current === controller && !controller.signal.aborted) setLoading(false); }
  }
  // Its list is read once for the person: their sign-in handed over or renewed keeps it (the next page goes with it).
  useEffect(() => { void load(); return () => { loader.current?.abort(); }; }, [source, view.dataSourceId, session.user.id]);
  // The window a choice opens stands where it stands (linkedViewWindowPlace).
  useEffect(() => { keepLinkedViewWindowPlace(menuRef.current, place); }, [place]);
  async function commit(ended: GraphView, settings: false | "new" | "picked") {
    if (working.current) return;
    working.current = true; setBusy(true); chosen.current = true;
    keepLinkedViewWindowPlace(menuRef.current, place);
    // A new view's window opens at its settings: its source is chosen already (sourceChosenView).
    if (settings === "new") markSourceChosenView(view.id);
    try { await onCommit(ended, settings); }
    finally { working.current = false; if (mounted.current) setBusy(false); }
  }
  const words = query.trim().toLowerCase();
  const matching = views.filter((saved) => !words || viewDisplayName(saved).toLowerCase().includes(words));
  const shown = allShown || words ? matching : matching.slice(0, SHOWN_VIEWS);
  // A view that waited in another layout makes that one, as Notion's "+ 새 보드 보기" (seen 2026-11-11).
  const newLabel = view.type === "table" ? t("databaseView:starterNewTableView") : t("databaseView:starterNewLayoutView", { layout: labels.viewTypes[view.type] });
  const makeNew = () => void commit(leftLinkedView(view), "new"), pick = (saved: GraphView) => void commit(viewTakenAfter(view, saved), "picked");
  if (phone) {
    return <PhoneLinkedViewSheet sourceTitle={sourceTitle} views={matching} loading={loading} failed={failed} busy={busy} more={Boolean(cursor) && !failed}
      newLabel={newLabel} onNewTable={makeNew} onPick={pick} onMore={() => void load(cursor)} onBack={back} onClose={leave} />;
  }
  return createPortal(<div ref={menuRef} role="dialog" aria-label={t("databaseView:starterNewView")} aria-busy={busy || undefined}
    className={`${styles.panel} ${docked ? dockColumn.column : ""}`} onKeyDown={keepLayerKeys} data-linked-database-starter="choose" data-docked={docked || undefined}
    style={docked ? { top: `max(var(--page-panel-top, var(--topbar-height)), ${dockTop ?? 0}px)` } : { position: "fixed", ...popoverPlacementStyle(place) }}>
    <header>{back ? <button type="button" className={styles.back} aria-label={t("common:actions.back")} disabled={busy}
      data-linked-starter-back onClick={back}><ArrowLeft size="small" /></button> : null}
      <strong>{t("databaseView:starterNewView")}</strong><button type="button" aria-label={t("common:actions.close")} disabled={busy} onClick={leave}><X size="small" /></button></header>
    {views.length > SHOWN_VIEWS ? <input ref={searchRef} type="search" value={query} placeholder={t("databaseView:starterSearchViews")} aria-label={t("databaseView:starterSearchViews")}
      disabled={busy} onChange={event => setQuery(event.target.value)} data-linked-starter-search /> : null}
    <button type="button" className={styles.choice} disabled={busy} data-linked-starter-new-table onClick={makeNew}><Plus size="regular" aria-hidden="true" />{newLabel}</button>
    {matching.length ? <p className={styles.group}>{t("databaseView:starterSourceViews", { source: sourceTitle || labels.untitled })}</p> : null}
    {shown.map(saved => <button key={saved.id} type="button" className={styles.choice} disabled={busy || loading} onClick={() => pick(saved)}><span className={styles.viewIcon}><ViewIcon view={saved} size="regular" /></span>{viewDisplayName(saved)}</button>)}
    {shown.length < matching.length ? <button type="button" className={styles.more} disabled={busy} onClick={() => setAllShown(true)}>{t("databaseView:starterMoreViews", { count: matching.length - shown.length })}</button> : null}
    {loading ? <p role="status">{t("editor:starter.loading")}</p> : null}
    {failed ? <p role="alert">{t("editor:starter.loadFailed")} <button type="button" onClick={() => void load(retryCursor.current)}>{t("editor:starter.retry")}</button></p> : null}
    {cursor && !failed ? <button type="button" disabled={busy || loading} onClick={() => void load(cursor)}>{t("formView:loadMore")}</button> : null}
  </div>, document.body);
}
