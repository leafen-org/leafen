import { useEffect, useRef, useState } from "react";
import { useStartChoosing } from "@/lib/startChoice";
import type { ContentGraph } from "@/contentGraph";
import { loadInlineDatabase } from "@/components/editor/InlineDatabaseBlock";
import { ProductLoadingScreen } from "@/components/loading/ProductLoadingScreen";
import styles from "./FirstScreenCover.module.css";

// How long the open page's body — kept here or read from the server — and the
// first screen's other parts may still take once the list is drawn before the
// workspace shows without them; the body's placeholder lines then show as on
// any page being opened.
const FIRST_BODY_MAX_MS = 600;
// The first screen's parts that come after the list and would move what is
// drawn when they come: a database's views (their own loading), the sidebar's
// sections until its teamspaces and layout are known, and the top bar's share
// face until the page's reach is known (2026-11-15: on a reload whose body was
// read, the body's scroll bar moved the page 7px left and the lock beside
// "공유" pushed the top bar's left part 18px, both after the cover lifted).
const FIRST_SCREEN_PARTS_PENDING = "[data-inline-database-loading], aside[data-app-sidebar]:not([data-sections-ready]), [data-first-screen-pending]";
// However the list's read ends, the cover lifts after this: the workspace
// then shows what it has, with its own loading and status, rather than a
// cover that never lifts (2026-10-16).
const FIRST_SCREEN_MAX_MS = 10_000;

/**
 * Covers the workspace until its first screen is drawn whole — the list and,
 * the open page's body, kept here or read, with its databases' views and the
 * parts that come after the list (FIRST_SCREEN_PARTS_PENDING) — so opening
 * shows it at once, as Notion's does, not a part at a time (2026-10-16: an
 * empty workspace, then its pages, then placeholder lines, then the body). A
 * body or part that takes longer than FIRST_BODY_MAX_MS shows as on any page
 * being opened. Until the list is drawn it is the loading screen, which stays
 * empty for a quiet moment. Only the first screen is covered.
 */
export function FirstScreenCover({ bodyPending, graph, listShown }: {
  /** Whether the open page's body is still being read or opened. */
  bodyPending: boolean;
  graph: ContentGraph;
  listShown: boolean;
}) {
  const [covering, setCovering] = useState(true);
  const [viewsReady, setViewsReady] = useState(false);
  const needsViews = graph.views.length > 0;
  // Started at the root, it waits too for the page the app opens with.
  const choosing = useStartChoosing();
  const drawn = !choosing && listShown && !bodyPending && (!needsViews || viewsReady);
  const drawnNow = useRef(drawn);
  drawnNow.current = drawn;

  useEffect(() => {
    if (!covering || !needsViews || viewsReady) return;
    let current = true;
    const ready = () => { if (current) setViewsReady(true); };
    void loadInlineDatabase().then(ready, ready);
    return () => { current = false; };
  }, [covering, needsViews, viewsReady]);

  useEffect(() => {
    if (!covering || !listShown || choosing) return;
    const timer = window.setTimeout(() => setCovering(false), FIRST_BODY_MAX_MS);
    return () => window.clearTimeout(timer);
  }, [covering, listShown, choosing]);

  useEffect(() => {
    if (!covering) return;
    const timer = window.setTimeout(() => setCovering(false), FIRST_SCREEN_MAX_MS);
    return () => window.clearTimeout(timer);
  }, [covering]);

  useEffect(() => {
    if (!covering || !drawn) return;
    // What was drawn is uncovered once its parts are on screen too.
    let frame = 0;
    const uncover = () => {
      if (!drawnNow.current) return;
      if (document.querySelector(FIRST_SCREEN_PARTS_PENDING)) frame = window.requestAnimationFrame(uncover);
      else setCovering(false);
    };
    frame = window.requestAnimationFrame(uncover);
    return () => window.cancelAnimationFrame(frame);
  }, [covering, drawn]);

  return covering ? <div className={styles.cover}><ProductLoadingScreen source="workspace" /></div> : null;
}
