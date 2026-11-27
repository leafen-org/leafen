import { i18next } from "@/i18n";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { pageNamesNow } from "@/lib/pageNames";
import type { Page } from "@/types";
import type { ViewType } from "@/lib/modelTypes/views";
import { viewDisplayName } from "../database/databaseViewShared";
import { propertyTypeLabel } from "../database/propertyTypes";
import type { PropertyType } from "@/lib/types";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

/** The kind a wiki's Home is listed with among the wiki's views in its updates (workspace_activity_database.go). */
export const WIKI_HOME_ACTIVITY_KIND = "wiki_home";

/**
 * What an update calls a thing that has no name, as Notion's updates call
 * it (seen 2026-11-13): a database "새 데이터베이스" in every line that
 * names it, a view by its layout ("표", "보드") — a wiki's Home "홈" (seen
 * 2026-11-17) — a property by its type, and a page as the page is called
 * everywhere else — a database page "새 데이터베이스", a page "새 페이지"
 * (pageDisplayTitle).
 */
export function untitledActivityName(entityType: string, kind: string, page?: Page, named?: PageNaming) {
  if (entityType === "dataSource") return untitledPageDisplayTitle({ kind: "database", viewBlock: false });
  if (entityType === "view" && kind === WIKI_HOME_ACTIVITY_KIND) return i18next.t("wikiView:home");
  if (entityType === "view") return viewDisplayName({ name: "", type: (kind || "table") as ViewType });
  if (entityType === "property" && kind) return propertyTypeLabel(kind as PropertyType);
  return untitledPageName(page, named);
}

type PageNaming = { kind?: string | null; viewBlock?: boolean; sourceTitles?: readonly DatabaseSourceTitle[] };

/**
 * A page's name when it has none: its database's own for a row, else its
 * kind's — one the reader's list doesn't hold by the kind the server says
 * (`named`), else a page's.
 */
export function untitledPageName(page?: Page, named?: PageNaming) {
  return page ? pageNamesNow.title({ ...page, title: "", titleSpans: undefined }) : untitledPageDisplayTitle(named ?? "page");
}
