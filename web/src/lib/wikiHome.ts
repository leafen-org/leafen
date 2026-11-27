import type { GraphBlock, GraphView } from "@/contentGraph";

/**
 * A wiki's Home is one of its views as Notion's is (seen 2026-11-11): the
 * address names it with `v=` as it names a view, and it is the view a person
 * last chose there, kept as a view's choice is (databaseViewChoice).
 */
export const WIKI_HOME_VIEW_ID = "home";

/**
 * Home's own name and its place among the wiki's views, which Notion keeps
 * for everyone as it keeps a view's (seen 2026-11-11: renaming Home and
 * dragging it below "모든 페이지" were saved to the wiki); here on the
 * wiki's database block. Empty, Home has no name of its own and stands first.
 */
export type WikiHome = { name: string; position: string };

export function wikiHomeOf(block: GraphBlock): WikiHome {
  const kept = block.content.wikiHome as { name?: unknown; position?: unknown } | undefined;
  return {
    name: typeof kept?.name === "string" ? kept.name : "",
    position: typeof kept?.position === "string" ? kept.position : "",
  };
}

/** Where Home stands among the views: before the first view placed after it, first while it has no place. */
export function wikiHomeIndex(views: readonly GraphView[], home: WikiHome) {
  if (!home.position) return 0;
  const after = views.findIndex((view) => view.position > home.position);
  return after < 0 ? views.length : after;
}

/**
 * The wiki's order once a row of its views list moved before or after
 * another, Home's row among them: each view at its new place and Home's
 * place, or null when nothing moved.
 */
export function wikiOrderAfterMove(
  views: readonly GraphView[], home: WikiHome, sourceId: string, targetId: string, side: "before" | "after",
): { views: GraphView[]; homePosition: string } | null {
  const ids = views.map(({ id }) => id);
  ids.splice(wikiHomeIndex(views, home), 0, WIKI_HOME_VIEW_ID);
  const from = ids.indexOf(sourceId);
  if (from < 0 || sourceId === targetId) return null;
  const ordered = [...ids];
  ordered.splice(from, 1);
  const to = ordered.indexOf(targetId);
  if (to < 0) return null;
  ordered.splice(to + (side === "after" ? 1 : 0), 0, sourceId);
  if (ordered.every((id, index) => id === ids[index])) return null;
  const place = (id: string) => String(ordered.indexOf(id)).padStart(6, "0");
  return {
    views: views.map((view) => ({ ...view, position: place(view.id) }))
      .sort((left, right) => left.position.localeCompare(right.position)),
    homePosition: place(WIKI_HOME_VIEW_ID),
  };
}
