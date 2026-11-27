import { FileText } from "@/icons/leafen";
import type { LeafenIconSize } from "@/icons/leafen/iconPrimitives";
import type { ViewType } from "@/lib/modelTypes/views";
import { ViewTypeIcon } from "../database/databaseViewShared";
import { WIKI_HOME_ACTIVITY_KIND } from "./activityNames";

/** A view's mark in an update's line: its layout's, or a wiki's Home's page, as its view list draws it (DatabaseViewTabs). */
export function ActivityViewIcon({ kind, size }: { kind: string; size: LeafenIconSize }) {
  return kind === WIKI_HOME_ACTIVITY_KIND ? <FileText size={size} aria-hidden="true" /> : <ViewTypeIcon type={(kind || "table") as ViewType} size={size} />;
}
