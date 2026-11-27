import { i18next } from "@/i18n";

/**
 * What database rows without a title are called, by one rule, the server's
 * (databaseUntitledRowName and database_untitled_row_kind.go): the name their
 * database keeps for them ("신규 작업" in a task tracker), else its untitled
 * default template's, else its kind's word (databaseKindRowName — "새 작업"
 * in a task database however it became one), else none, a new page. By data
 * source, from each read of a page's graph, its kind's word in the reader's
 * language; and by row, from lists that hold rows of several databases under
 * one, as "내 작업" does, as the server named them. pageDisplayTitle reads
 * them for a row, so its path, a sub-page's path below it, the sidebar, a
 * related row, a sub-item's parent or a grouped row is called as its
 * database's views and its own title's place call it.
 */
type UntitledRowKinds = {
  taskDatabase?: boolean; sprintStatusPropertyId?: string; projectDatabase?: boolean; meetingDatabase?: boolean; docsDatabase?: boolean;
};

/** The data sources a graph read gave: the name the server read for their rows (`untitledRowName`) and their kinds. */
const sources = new Map<string, { name: string; kinds: UntitledRowKinds }>();

/** Rows a list gave, each with the name the server called it by while untitled. */
const rowNames = new Map<string, string>();

/**
 * The names databases keep for their pages without a title, by data source
 * (a task tracker's "신규 작업"): they go before its templates, as Notion's
 * item name does whichever template is default (seen 2026-11-11).
 */
const ownNames = new Map<string, string>();

/**
 * A database's kind's word for its rows without a title, in the reader's
 * language, or "" — in the server's order (databaseUntitledRowKind): "새
 * 스프린트" in a Sprints database, "새 프로젝트" in one made as projects,
 * "신규 회의" in one made as meeting notes (Notion's page title, window and
 * path, seen 2026-11-11, its table cell blank), "새 문서" in one from the docs
 * hub, "새 작업" in a task database — Notion's turned into one by hand calls
 * an untitled row so in its title's place, window, path, a sub-page's path
 * below it, the sidebar and its template menu (seen 2026-11-26).
 */
export function databaseKindRowName(source: UntitledRowKinds) {
  if (source.sprintStatusPropertyId) return String(i18next.t("databaseView:newSprint"));
  if (source.projectDatabase) return String(i18next.t("databaseView:newProject"));
  if (source.meetingDatabase) return String(i18next.t("databaseView:untitledMeeting"));
  if (source.docsDatabase) return String(i18next.t("databaseView:newDoc"));
  return source.taskDatabase ? String(i18next.t("databaseView:newTask")) : "";
}

/** What a data source a graph read gave calls its rows without a title: the server's read, else its kind's word. */
export function sourceUntitledRowName(source: UntitledRowKinds & { untitledRowName?: string }) {
  return source.untitledRowName?.trim() || databaseKindRowName(source);
}

/** The data sources a graph read gave, each with its rows' name or none and its kinds. */
export function rememberDataSourceUntitledRowNames(sources_: readonly (UntitledRowKinds & { id: string; untitledRowName?: string; untitledName?: string })[]) {
  for (const source of sources_) {
    const own = source.untitledName?.trim();
    sources.set(source.id, {
      name: source.untitledRowName?.trim() ?? "",
      kinds: {
        taskDatabase: source.taskDatabase, sprintStatusPropertyId: source.sprintStatusPropertyId, projectDatabase: source.projectDatabase,
        meetingDatabase: source.meetingDatabase, docsDatabase: source.docsDatabase,
      },
    });
    if (own) ownNames.set(source.id, own);
    else ownNames.delete(source.id);
  }
}

/** The name a database keeps for its pages without a title, by its data source's ID, or "". */
export function databaseOwnUntitledName(dataSourceId?: string | null) {
  return (dataSourceId && ownNames.get(dataSourceId)) || "";
}

/** Rows a list gave, each with the name it goes by while untitled. */
export function rememberRowUntitledNames(rows: readonly { id: string; untitledName?: string }[]) {
  for (const row of rows) {
    const name = row.untitledName?.trim();
    if (name) rowNames.set(row.id, name);
  }
}

/**
 * What an untitled database row is called, by its database's read — its kind's
 * word in the reader's language, before a list's word for it — else by its own
 * ID; "" for any other page or one whose database neither names.
 */
export function untitledRowNameOf(page: { id?: string; kind?: string; parentType?: string | null; parentId?: string | null }) {
  if (page.kind === "database") return "";
  const source = page.parentType === "database" && page.parentId ? sources.get(page.parentId) : undefined;
  return (source && (source.name || databaseKindRowName(source.kinds))) || (page.id && rowNames.get(page.id)) || "";
}
