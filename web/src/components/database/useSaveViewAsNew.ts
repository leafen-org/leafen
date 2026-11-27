import { useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { useProductToast } from "@/lib/productToast";
import type { DatabaseViewOperationContext, DatabaseViewOperations, DatabaseViewSnapshot } from "./DatabaseView";
import { viewDisplayName } from "./databaseViewShared";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * "새 보기로 저장", as Notion's (seen 2026-10-24 at 375 and 1280px): the
 * viewer's own filters and sorts go to a new view of the same layout named
 * "<name> (1)", which opens with its settings and its name chosen, as a
 * copied view does; the view they were on goes back to what it saves. Then
 * "모두에게 필터가 저장되었습니다. · 실행 취소" (seen 2026-11-02); undoing
 * goes back to that view, with the viewer's own filters and sorts, and
 * deletes the new one.
 */
export function useSaveViewAsNew({ snapshot, operations, signal, effectiveView, resetOwnQuery, setOwnQuery, openSettings }: {
  snapshot: DatabaseViewSnapshot;
  operations: DatabaseViewOperations;
  signal: AbortSignal;
  effectiveView(view: GraphView): GraphView;
  resetOwnQuery(viewId: string): void;
  setOwnQuery(view: GraphView): void;
  openSettings(): void;
}) {
  const { t } = useTranslation("databaseView");
  const { notify } = useProductToast();
  // Each step asks as of the database it then shows: making the view changes it.
  const latest = useRef({ snapshot, setOwnQuery });
  latest.current = { snapshot, setOwnQuery };
  return useCallback(async (view: GraphView) => {
    const context = (requestKey: string): DatabaseViewOperationContext => ({
      contextIdentity: latest.current.snapshot.contextIdentity, dataSourceId: latest.current.snapshot.dataSource.id,
      requestGeneration: latest.current.snapshot.requestGeneration, requestKey, signal, snapshotIdentity: latest.current.snapshot.snapshotIdentity,
      viewId: latest.current.snapshot.activeView.id,
    });
    // As a copied view: shown at once with its name and settings, chosen on
    // the press's own frame, and written behind (user decision 2026-11-12).
    let shown = false;
    const made = (created: GraphView) => {
      if (shown) return;
      shown = true;
      const taken = new Set(latest.current.snapshot.views.map(viewDisplayName));
      let copies = 1;
      while (taken.has(`${viewDisplayName(view)} (${copies})`)) copies += 1;
      const seen = effectiveView(view);
      const saved = { ...created, name: `${viewDisplayName(view)} (${copies})`, config: { ...seen.config } };
      const configured = context(`view:save-as-new-config:${created.id}`);
      void operations.updateViews([saved], configured).catch((error) => operations.onError(error, configured));
      void operations.selectView(created, context(`view:select:${created.id}`));
      resetOwnQuery(view.id);
      notify(t("toast.savedFiltersForEveryone"), "success", { label: t("undo"), onClick: async () => {
        try {
          await operations.selectView(view, context(`view:select:${view.id}`));
          latest.current.setOwnQuery(seen);
          await operations.deleteView(saved, context(`view:delete:${saved.id}`));
        } catch (error) {
          operations.onError(error, context(`view:save-as-new-undo:${saved.id}`));
        }
      } });
      // The settings open a frame on, their name chosen the frame after, what is typed meanwhile going in.
      window.requestAnimationFrame(() => openSettings());
      focusOpenedField(() => document.querySelector<HTMLInputElement>("[data-view-settings-name]"), { select: true, from: 2, frames: 10 });
    };
    try {
      const created = await operations.createView(view.type, context(`view:save-as-new:${view.id}`), made);
      if (created) made(created);
    } catch (error) {
      operations.onError(error, context(`view:save-as-new:${view.id}`));
    }
  }, [effectiveView, notify, openSettings, operations, resetOwnQuery, signal, t]);
}
