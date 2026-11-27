import { useEffect, useRef } from "react";
import type { GraphView } from "@/contentGraph";
import { listenSourceFirstView, listenViewDeletion, markSourceFirstView } from "@/lib/viewTabRequests";

// A view made for a request is the one that shows within this long of it.
const MADE_WITHIN_MS = 5000;

/**
 * What a database's tabs do when asked from elsewhere (lib/viewTabRequests):
 * a form's "양식 삭제" deletes its view while another view shows its source,
 * and "기존 데이터 소스 연결하기" makes a new table view, the view that then
 * shows marked to choose its source first.
 */
export function useViewTabRequests({ databaseId, views, readOnly, atLimit, deleteView, createView }: {
  databaseId?: string;
  views: readonly GraphView[];
  readOnly: boolean;
  atLimit: boolean;
  deleteView(view: GraphView): Promise<unknown>;
  createView(type: "table"): void;
}) {
  useEffect(() => listenViewDeletion((viewId) => {
    const view = views.find(({ id }) => id === viewId);
    if (!view || readOnly || views.filter(({ dataSourceId }) => dataSourceId === view.dataSourceId).length < 2) return false;
    void deleteView(view);
    return true;
  }));
  const asked = useRef<{ before: Set<string>; at: number } | null>(null);
  useEffect(() => listenSourceFirstView((id) => {
    if (readOnly || atLimit || !databaseId || id !== databaseId) return false;
    asked.current = { before: new Set(views.map((view) => view.id)), at: performance.now() };
    createView("table");
    return true;
  }));
  useEffect(() => {
    const request = asked.current;
    if (!request) return;
    if (performance.now() - request.at > MADE_WITHIN_MS) { asked.current = null; return; }
    const made = views.find(({ id }) => !request.before.has(id));
    if (!made) return;
    asked.current = null;
    markSourceFirstView(made.id);
  }, [views]);
}
