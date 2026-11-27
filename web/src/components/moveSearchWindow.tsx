import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ListState } from "react-stately";
import { ArrowDown } from "@/icons/leafen";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import { workspaceSearchBounds } from "@/workspaceSearchWire.generated";
import { MOVE_SEARCH_LIMIT } from "./pageMoveDialogModel";
import styles from "./MoveToDialog.module.css";

/** The key of a move search's "더 불러오기" among its rows. */
export const MOVE_LOAD_MORE_KEY = "move:load-more";

/**
 * How many of a move search's answers show: twenty, and twenty more at each
 * "더 불러오기", as Notion's move window (seen 2026-11-19 in two Chrome tabs:
 * 26 pages found showed 20 then all 26, 45 showed 20, 40, then all 45, the
 * row gone once every answer showed); another search starts at twenty again.
 * `remoteLimit` is how many pages the server's search is asked for, a window
 * ahead of those shown, as a search's answers that name the query in their
 * text alone are not a move's answers (moveDestinations).
 */
export function useMoveSearchWindow(query: string) {
  const key = foldNfcText(query.trim());
  const [state, setState] = useState({ key, shown: MOVE_SEARCH_LIMIT });
  // Another search forgets the answers loaded for the last (coming back to its words starts at twenty too).
  if (state.key !== key) setState({ key, shown: MOVE_SEARCH_LIMIT });
  const shown = state.key === key ? state.shown : MOVE_SEARCH_LIMIT;
  const loadMore = useCallback(() => setState({ key, shown: shown + MOVE_SEARCH_LIMIT }), [key, shown]);
  return { shown, loadMore, remoteLimit: Math.min(workspaceSearchBounds.results, shown + MOVE_SEARCH_LIMIT) };
}

/** The answers a move search shows of those it found (all of them before a search), and whether more wait behind "더 불러오기". */
export function moveSearchShown<T>(found: readonly T[], searching: boolean, shown: number) {
  return searching && found.length > shown ? { found: found.slice(0, shown), more: true } : { found, more: false };
}

/**
 * A move search's "더 불러오기" under its answers, a row the arrows reach as
 * the answers', as Notion's: its 16px arrow 4px in and its words 8px after,
 * both in the faint grey (seen 2026-11-19 at 1440, dark: rgb(125,122,117)).
 */
export function MoveLoadMoreRow<T>({ state }: { state: ListState<T> }) {
  const { t } = useTranslation("moveToDialog");
  return (
    <NativePickerOption state={state} itemKey={MOVE_LOAD_MORE_KEY} className={`${styles.result} ${styles.loadMore}`} data-move-load-more="">
      {({ labelProps }) => <>
        <ArrowDown size="small" aria-hidden="true" />
        <span {...labelProps}>{t("moveToDialog:results.loadMore")}</span>
      </>}
    </NativePickerOption>
  );
}
