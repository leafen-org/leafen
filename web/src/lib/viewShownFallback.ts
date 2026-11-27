import type { GraphView } from "@/contentGraph";

// A view keeps naming a property that is deleted (the server's
// database_property_trash_views.go): a board grouped by it, or a calendar or
// timeline dated by it, draws its fallback and, once shown, keeps it for
// good, as Notion's (seen 2026-11-08). ⌘Z right after the deletion takes
// that back with the property, as Notion's one ⌘Z did.

/** What a view's fallback rewrites: a board's grouping, a calendar's or timeline's date. */
function shownKeys(view: GraphView): readonly string[] {
  if (view.type === "board") return ["groupBy", "groupOrder", "groupSort", "groupByMode", "hideEmptyGroups", "hiddenGroups"];
  if (view.type === "calendar") return ["calendarBy"];
  if (view.type === "timeline") return named(view, "timelineBy") ? ["timelineBy"] : ["calendarBy"];
  return [];
}

function named(view: GraphView, key: string) {
  const value = view.config?.[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

/** The property a board groups by, or a calendar or timeline dates by, as its settings name it. */
export function viewShownBy(view: GraphView): string | undefined {
  const key = shownKeys(view)[0];
  return key ? named(view, key) : undefined;
}

/** The views shown by one of the properties, as they are before those are deleted. */
export function viewsShownBy(views: readonly GraphView[], propertyIds: readonly string[]): GraphView[] {
  return views.filter((view) => {
    const by = viewShownBy(view);
    return by !== undefined && propertyIds.includes(by);
  });
}

/**
 * The views to write back once the properties are restored: those that took
 * a fallback meanwhile get their own grouping or date again, the rest of
 * their settings as they are now.
 */
export function viewsShownAgain(before: readonly GraphView[], now: readonly GraphView[]): GraphView[] {
  return before.flatMap((saved) => {
    const current = now.find((view) => view.id === saved.id);
    if (!current || current.type !== saved.type || viewShownBy(current) === viewShownBy(saved)) return [];
    const config: Record<string, unknown> = { ...current.config };
    for (const key of shownKeys(saved)) {
      if (key in saved.config) config[key] = saved.config[key];
      else delete config[key];
    }
    return [{ ...current, config }];
  });
}
