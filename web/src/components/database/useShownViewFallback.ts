import { useEffect, useRef } from "react";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { viewShownBy } from "@/lib/viewShownFallback";
import { boardGroupDefault } from "./viewLayoutDefaults";
import { firstViewDateSource } from "./viewDateSource";

type ShownProperty = Pick<GraphProperty, "id" | "type" | "position" | "publicOrder"> & { config?: unknown };

/**
 * What a shown view would keep, as Notion's screen writes it once shown
 * (seen 2026-11-08, 2026-11-12): a board, calendar or timeline naming no
 * property it groups or dates by keeps the default it draws
 * (boardGroupPropertyId, CalendarView, TimelineView), and one naming a
 * property missing here keeps its fallback when the property is in Deleted
 * properties. A timeline's zoom and centre are not written. A key
 * for the ask, or none when there is nothing to keep.
 */
function shownAsk(view: GraphView, properties: readonly ShownProperty[]): string | undefined {
  if (view.type !== "board" && view.type !== "calendar" && view.type !== "timeline") return undefined;
  const by = viewShownBy(view);
  if (by !== undefined) return properties.some((property) => property.id === by) ? undefined : by;
  const fallback = view.type === "board" ? boardGroupDefault(properties) : firstViewDateSource(properties)?.id;
  return fallback ? "default" : undefined;
}

/**
 * Tells the server a view was shown when it has something to keep
 * (shownAsk): the server decides from what it holds and writes it
 * (databaseViewShownConfig). Asked once for each thing while it is missing
 * here.
 */
export function useShownViewFallback(
  view: GraphView,
  properties: readonly ShownProperty[],
  viewShown: ((view: GraphView) => Promise<void>) | undefined,
) {
  const ask = shownAsk(view, properties);
  const asked = useRef("");
  useEffect(() => {
    if (!ask) {
      asked.current = "";
      return;
    }
    const key = `${view.id}/${ask}`;
    if (!viewShown || asked.current === key) return;
    asked.current = key;
    void viewShown(view).catch(() => {
      if (asked.current === key) asked.current = "";
    });
  }, [ask, view, viewShown]);
}
