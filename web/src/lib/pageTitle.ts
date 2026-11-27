import { i18next } from "@/i18n";
import type { Page } from "@/types";
import type { TextSpan } from "@/lib/types";
import { untitledRowNameOf } from "./untitledRowNames";
import { titleWithClosedMentions } from "./closedMentions";
import { untitledDatabaseSourcesName, type DatabaseSourceTitle } from "@/components/editor/databaseTitles";

type PageWithOptionalProperties = Pick<Page, "title"> & {
  properties?: Record<string, unknown>;
};

/**
 * What an untitled page is called, by its kind or the page itself: "새
 * 페이지", and a full-page database "새 데이터베이스" in its title, window
 * and path, as Notion's (seen 2026-10-01 and 2026-10-10) — but "제목 없음"
 * where that database is a view block, a dashboard or a linked view with no
 * database of its own, as Notion names the page one becomes (seen
 * 2026-11-09: its links, path and window; the server says which, viewBlock).
 * A list's row the page list may not hold says its page's kind itself
 * (`pageKind`/`kind` and `viewBlock`, internal/app/page_name_kind.go). One
 * holding two or more sources of its own is named by them listed, "새 데이터
 * 소스 및 새 데이터 소스", as Notion's title, sidebar row, path and window name it
 * (seen 2026-11-18; the page list says them, `sourceTitles`,
 * full_page_database_sources_name.go) — the one left by the trash alone
 * naming it (seen 2026-11-19).
 * Every caller says what it names — the page, its kind, or "page" for what
 * can only be a page (a row's, a formula's) — so none names a database as a
 * page by leaving it out.
 */
export function untitledPageDisplayTitle(
  page: Page["kind"] | { kind?: string | null; viewBlock?: boolean; sourceTitles?: readonly DatabaseSourceTitle[] } | null | undefined,
) {
  const { kind, viewBlock, sourceTitles } = typeof page === "object" && page ? page : { kind: page ?? undefined, viewBlock: false, sourceTitles: undefined };
  if (kind === "database" && viewBlock) return String(i18next.t("common:labels.untitled"));
  if (kind === "database" && sourceTitles?.length) return untitledDatabaseSourcesName(sourceTitles);
  return String(i18next.t(kind === "database" ? "databaseTitles:inlineDatabasePlaceholder" : "common:labels.newPage"));
}

/**
 * A page's name: its title as written — one of spaces alone is a title,
 * shown blank, as Notion shows it in its sidebar, links, path and window
 * (seen 2026-11-13: "   ") — or while it has none what it is called — a
 * database row as its database calls its untitled rows, by its database's
 * read where the app has one (untitledRowNames.ts, its kind's word in the
 * reader's language), else as the server named it (`untitledName`), as
 * Notion's task tracker calls its untitled rows "신규 작업" in search, the
 * trash, a relation's picker and chips and its path (seen 2026-11-07 and
 * 2026-11-08) and a task database "새 작업" (seen 2026-11-26) — else what an
 * untitled page of its kind is called.
 */
export function pageDisplayTitle(page: (Pick<Page, "title"> & {
  id?: string; kind?: Page["kind"]; viewBlock?: boolean; sourceTitles?: readonly DatabaseSourceTitle[]; untitledName?: string; parentType?: string | null; parentId?: string | null;
  titleSpans?: readonly TextSpan[];
}) | undefined | null) {
  // Its mentions as a plain title says them, by its spans (closedMentions.ts).
  const title = titleWithClosedMentions(page?.title ?? "", page?.titleSpans);
  return title || (page ? untitledRowNameOf(page) : "") || page?.untitledName?.trim() || untitledPageDisplayTitle(page);
}

/**
 * What a list's row says of the page it names apart from the reader's page
 * list (`pageKind`, `viewBlock`, `sourceTitles`, internal/app/page_name_kind.go),
 * as untitledPageDisplayTitle reads it: an untitled full-page database of
 * several sources named by them there too, as Notion's search lists it (seen
 * 2026-11-18).
 */
export function listedPageNameKind(row: { pageKind?: string | null; viewBlock?: boolean; sourceTitles?: readonly DatabaseSourceTitle[] } | null | undefined) {
  return { kind: row?.pageKind, viewBlock: row?.viewBlock, sourceTitles: row?.sourceTitles };
}

/**
 * A page named where the reader's page list may not hold it — an inbox
 * notice's, an access request's, a sidebar shortcut's: as its page names it
 * where the list knows it, an untitled full-page database "새 데이터베이스"
 * as Notion's notices name it (seen 2026-11-13); else by the title the
 * server read for it; else by the kind the server says of it (`named`,
 * page_name_kind.go); else, its kind untold (a page the reader can't open),
 * what an untitled page is called.
 */
export function listedPageDisplayTitle(page: Parameters<typeof pageDisplayTitle>[0], title = "", titleSpans?: readonly TextSpan[],
  named?: { kind?: string | null; viewBlock?: boolean; sourceTitles?: readonly DatabaseSourceTitle[] } | null) {
  return (page ? pageDisplayTitle(page) : "") || titleWithClosedMentions(title, titleSpans) || untitledPageDisplayTitle(named ?? "page");
}

function linkedDatabaseResolvedTitle(
  page: PageWithOptionalProperties | undefined | null
) {
  const value = page?.properties?.notionLinkedDatabaseResolvedTitle;
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

export function databaseDisplayTitle(
  page: PageWithOptionalProperties | undefined | null
) {
  return linkedDatabaseResolvedTitle(page) ?? pageDisplayTitle(page);
}
