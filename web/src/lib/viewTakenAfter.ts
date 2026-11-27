import type { GraphView } from "@/contentGraph";
import { withoutLinkedStarter } from "@/lib/databaseStarterLinked";

/**
 * A view taking after one of its source's views: that view's layout, its
 * name just as it was typed and its settings, without what marked it waiting
 * in "새 보기". Picked in a linked view's "새 보기" (DatabaseLinkedStarter)
 * or under a new view's "〈소스〉의 조회수" (adoptSourceViewOperations), as
 * Notion's both ways: a view named "  zl  " is copied "  zl  ", its spaces
 * kept (seen 2026-11-18 in Chrome 1440, in two tabs at different times, read
 * after the pick only). A view turned into a dashboard gives its first widget
 * the same copy (turnViewIntoDashboard), as Notion's: "  w1956 보기  " and
 * " w1956 둘째   " became widgets of those very names (2026-11-18, two tabs
 * at different times, read after the change and again after a reload).
 */
export function viewTakenAfter(view: GraphView, saved: GraphView): GraphView {
  return { ...view, type: saved.type, name: saved.name, config: withoutLinkedStarter(saved.config) };
}
