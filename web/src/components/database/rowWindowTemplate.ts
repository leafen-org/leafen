import { useEffect, useRef, useSyncExternalStore } from "react";
import { pushRoute, ROUTE_CHANGE_EVENT } from "@/lib/router";
import { databaseViewUrlState, databaseViewUrlWithSelection } from "./databaseViewModel";

/**
 * A template opened from a view inside a row's window — beside the page or in
 * its middle — takes the window's place, as Notion's (seen 2026-11-16, an
 * inline database in a row's page, side and middle alike): the row's window
 * goes and the template's opens alone, the address naming the template
 * (Leafen's `tp=`) with the row kept beneath it (`pp`), so back brings the
 * row's window again and forward the template's; the template closed — its
 * Escape, ✕ or a press beside it — leaves the page alone, a step of its own.
 * The row's window stays drawn hidden while the template stands in for it,
 * since the view that opened the template lives in it.
 */

const hidden = new Set<string>();
const listeners = new Set<() => void>();

function setHidden(rowId: string, on: boolean) {
  if (hidden.has(rowId) === on) return;
  if (on) hidden.add(rowId);
  else hidden.delete(rowId);
  for (const listener of [...listeners]) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Whether a row's window is hidden under a template opened from inside it. */
export function useRowWindowUnderTemplate(rowId: string) {
  return useSyncExternalStore(subscribe, () => hidden.has(rowId), () => false);
}

/** Whether the address shows a template over this row's window, the row kept beneath it. */
function templateOverRow(url: URL, param: string, rowId: string) {
  const state = databaseViewUrlState(url);
  return !state.rowId && state.openAbove.at(-1)?.rowId === rowId ? url.searchParams.get(param) : null;
}

/**
 * The address of a template a view in a row's window opens, as Notion's
 * (useTemplateAddress): where the address names that row's window beside or
 * in the middle, the template in its place; elsewhere none, the template's
 * window opening over the window it came from.
 */
export function useRowWindowTemplateAddress({ param, rowId, templateIds, openId, open, claim }: {
  param: string;
  /** The database page whose window holds the view, if any. */
  rowId: string | undefined;
  templateIds: { current: readonly string[] };
  openId: string | null;
  open: { current: (templateId: string | null) => void };
  claim(templateId: string): boolean;
}) {
  // Whether the template shown is the address's, in the row window's place.
  const addressed = useRef<string | null>(null);
  const key = templateIds.current.join();
  // Reloaded or come back to, the address's template opens again over the row beneath it.
  useEffect(() => {
    if (!rowId) return undefined;
    const follow = () => {
      const named = templateOverRow(new URL(window.location.href), param, rowId);
      const ours = named && templateIds.current.includes(named) ? named : null;
      if (addressed.current && addressed.current !== ours) open.current(null);
      else if (!addressed.current && ours && claim(ours)) open.current(ours);
    };
    follow();
    window.addEventListener("popstate", follow);
    window.addEventListener(ROUTE_CHANGE_EVENT, follow);
    return () => { window.removeEventListener("popstate", follow); window.removeEventListener(ROUTE_CHANGE_EVENT, follow); };
  }, [claim, key, open, param, rowId, templateIds]);
  useEffect(() => {
    if (!rowId) return undefined;
    const url = new URL(window.location.href);
    const was = addressed.current;
    if (openId && openId !== was) {
      const state = databaseViewUrlState(url);
      if (state.rowId === rowId && state.rowMode !== "full") {
        // Opened by a press: the template in the row window's place, the row beneath it.
        const next = databaseViewUrlWithSelection(url, { rowId: null,
          openAbove: [...state.openAbove, { rowId, rowMode: state.rowMode, viewId: state.viewId ?? "" }] });
        next.searchParams.set(param, openId);
        window.history.pushState(window.history.state, "", `${next.pathname}${next.search}${next.hash}`);
      } else if (was && templateOverRow(url, param, rowId) === was) {
        // One opened from the template shown (its copy) takes its place in turn.
        url.searchParams.set(param, openId);
        window.history.pushState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
      } else if (templateOverRow(url, param, rowId) !== openId) {
        addressed.current = null;
        return undefined;
      }
      addressed.current = openId;
      claim(openId);
      setHidden(rowId, true);
    } else if (!openId && was) {
      addressed.current = null;
      // Closed here rather than by back: the page alone, the row's window going too.
      if (templateOverRow(url, param, rowId) === was) {
        const next = databaseViewUrlWithSelection(url, { rowId: null });
        next.searchParams.delete(param);
        pushRoute(`${next.pathname}${next.search}${next.hash}`);
      }
      setHidden(rowId, false);
    }
    return undefined;
  }, [claim, openId, param, rowId]);
  useEffect(() => () => { if (rowId && addressed.current) setHidden(rowId, false); }, [rowId]);
}
