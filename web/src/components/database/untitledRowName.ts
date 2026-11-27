import { createContext, useCallback, useContext } from "react";
import { i18next } from "@/i18n";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { databaseKindRowName, databaseOwnUntitledName, untitledRowNameOf } from "@/lib/untitledRowNames";
import { newSprintName } from "@/lib/sprintRows";
import type { Page } from "@/lib/types";
import { plainTitleOf } from "@/lib/closedMentions";
import { toolbarTemplateName } from "./databaseToolbarModel";

/** Whether the database shown is a task database (NativeDatabaseViewMount). */
export const DatabaseTaskRowsContext = createContext(false);

/**
 * The properties a task database works by, its status, assignee and due date:
 * their type stays and they can't be deleted, as Notion's (seen 2026-11-05).
 */
export const DatabaseTaskRolePropertiesContext = createContext<ReadonlySet<string>>(new Set());

/**
 * A source's own properties, kept as they are, where its source says so:
 * People's five (databaseSourceRules.tsx). Their column's menu and settings have
 * no name, kind, settings, copy or delete — their view's rows alone — as
 * Notion's People shows its owner (seen 2026-11-15).
 */
export const DatabaseFixedPropertiesContext = createContext<ReadonlySet<string>>(new Set());

/** Whether the database shown is a Sprints database (NativeDatabaseViewMount). */
export const DatabaseSprintRowsContext = createContext(false);

/** Whether the database shown was made as projects (NativeDatabaseViewMount). */
export const DatabaseProjectRowsContext = createContext(false);

/** Whether the database shown was made as meeting notes (NativeDatabaseViewMount). */
export const DatabaseMeetingRowsContext = createContext(false);

/** Whether the database shown was made from the docs hub template (NativeDatabaseViewMount). */
export const DatabaseDocsRowsContext = createContext(false);

/**
 * The name the shown database keeps for its pages without a title, by itself
 * (its data source's `untitledName`, NativeDatabaseViewMount), or "".
 */
export const DatabaseOwnUntitledNameContext = createContext("");

/**
 * What a new row of the database shown is called: "새 작업" in a task
 * database — "+ 새 작업" (seen 2026-11-02) — "새 스프린트" in a Sprints
 * database (seen 2026-11-04), "새 프로젝트" in a database made as projects
 * (seen 2026-11-05), "새 회의" in one made as meeting notes, "새 문서" in one
 * made from the docs hub (seen 2026-11-11), else the name the database keeps
 * for its untitled pages — Notion's brainstorm, whose item name is its own
 * word rather than one of its kinds, reads "+ 신규 아이디어" (seen
 * 2026-11-12) — else "새 페이지". An untitled row goes by its database's
 * name for them, its kind's word last (DatabaseRowTemplateNameContext,
 * untitledRowNames.ts), which this names too where none is shown.
 */
export function useUntitledRowName() {
  const tasks = useContext(DatabaseTaskRowsContext), sprints = useContext(DatabaseSprintRowsContext);
  const projects = useContext(DatabaseProjectRowsContext), meetings = useContext(DatabaseMeetingRowsContext);
  const docs = useContext(DatabaseDocsRowsContext), own = useContext(DatabaseOwnUntitledNameContext);
  if (sprints) return newSprintName();
  if (projects) return String(i18next.t("databaseView:newProject"));
  if (meetings) return String(i18next.t("databaseView:newMeeting"));
  if (docs) return String(i18next.t("databaseView:newDoc"));
  return tasks ? String(i18next.t("databaseView:newTask")) : own || untitledPageDisplayTitle("page");
}

/**
 * What the shown database calls its pages without a title, by the name it
 * keeps for them or its untitled default template's (databaseUntitledName,
 * NativeDatabaseViewMount), else its kind's word (databaseKindRowName):
 * Notion's task tracker's cards read "신규 작업" while its "+ 새 작업" stays
 * (seen 2026-11-08).
 */
export const DatabaseRowTemplateNameContext = createContext("");

/**
 * A row's name as the database shown calls it: after its default template
 * while it has no name of its own — the row's, the shown database's, or, in
 * a list of several databases' rows, the one its own database goes by.
 */
export function useRowDisplayTitle() {
  const untitled = useUntitledRowName(), template = useContext(DatabaseRowTemplateNameContext);
  // The same function while those names are, so what a view works out from its rows' names is kept between drawings.
  return useCallback((row: Pick<Page, "title" | "titleSpans"> & { id?: string; kind?: Page["kind"]; viewBlock?: boolean; untitledName?: string; parentType?: Page["parentType"]; parentId?: Page["parentId"] }) =>
    plainTitleOf(row) || (row.kind === "database" ? untitledPageDisplayTitle(row)
      : row.untitledName?.trim() || template || untitledRowNameOf(row) || untitled), [template, untitled]);
}

/**
 * What a database calls a page of its own with no title, row and template
 * alike: the name it keeps for them (a task tracker's "신규 작업", by its data
 * source's ID), else its default template's name while that template's page
 * has no title of its own, else "" — then what a new row of it is called.
 * Notion names them by its database's own word for its items, whichever
 * template is default (seen 2026-11-11: in its task tracker a new template
 * made with no name reads "신규 작업" in the template menu, its title's
 * place, path and window, as its untitled rows do, with "Tmpl B" made
 * default too; where the titled "Tmpl A" is the default of a database
 * without such a word, an untitled row and a new template read "새 페이지"):
 * a titled default template gives the rows it makes its title, and names
 * none.
 */
export function databaseUntitledName(templates: readonly { name: string; title?: string; titled?: boolean; isDefault?: boolean }[],
  dataSourceId?: string | null) {
  const own = databaseOwnUntitledName(dataSourceId);
  if (own) return own;
  const fallback = templates.find((candidate) => candidate.isDefault);
  return !fallback || (fallback.titled ?? Boolean(fallback.title?.trim())) ? "" : fallback.name.trim();
}

/**
 * What the database shown calls a page of its own with no title, row or
 * template (databaseUntitledName): by the data source given, the database
 * shown, or the templates given, else as a new row of it is called.
 */
export function useDatabaseUntitledName(templates: readonly { name: string; title?: string; titled?: boolean; isDefault?: boolean }[] = [],
  dataSourceId?: string) {
  const shown = useContext(DatabaseRowTemplateNameContext), rowName = useUntitledRowName();
  return (dataSourceId ? databaseUntitledName(templates, dataSourceId)
    || untitledRowNameOf({ parentType: "database", parentId: dataSourceId }) : "")
    || shown || databaseUntitledName(templates) || rowName;
}

/** A template's name, as its database's menus and lists show it: its page's title, the name it was made with, else its database's for an untitled page. */
export function useTemplateDisplayName(templates: readonly { name: string; title?: string; titled?: boolean; isDefault?: boolean }[] = [],
  dataSourceId?: string) {
  const untitled = useDatabaseUntitledName(templates, dataSourceId);
  return (template: { name: string; title?: string }) => toolbarTemplateName(template) || untitled;
}

/**
 * What a database page with no title is called on itself — its title's place,
 * its path and its window: a row by its database's name for untitled pages
 * (databaseUntitledName: the name it keeps, else as its template list holds
 * it now), else as its database's read names its untitled rows, its kind's
 * word in the reader's language (untitledRowNames.ts), else by the name it
 * was read with (`untitledName`), else as any untitled row of it is
 * (`rowName`). A template being edited goes by the name it was made with,
 * as its database's menus call it — Notion's task tracker's "신규 작업" opened
 * full reads so in its path, window and title's place (seen 2026-11-11) — and
 * one made with none as its database's untitled pages, as the menus name it.
 */
export function untitledRowPageName({ template, untitledName, rowId, dataSourceId, rowName, templates }: {
  template: boolean; untitledName?: string; rowId: string; dataSourceId: string; rowName: string;
  templates: readonly { name: string; titled?: boolean; isDefault?: boolean }[];
}) {
  const named = untitledName?.trim(), database = databaseUntitledName(templates, dataSourceId);
  if (template) return named || database || untitledRowNameOf({ parentType: "database", parentId: dataSourceId }) || rowName;
  return database || untitledRowNameOf({ id: rowId, parentType: "database", parentId: dataSourceId }) || named || rowName;
}

/**
 * What a row page names its untitled row away from its title's place — its
 * window, its path and its menus — as its title's place does
 * (untitledRowPageName), by what its read says of its database.
 */
export function rowPageUntitledName(snapshot: { untitledName?: string; taskDatabase?: boolean; template?: boolean; row: { id: string; dataSourceId: string } }) {
  return untitledRowPageName({ template: Boolean(snapshot.template), untitledName: snapshot.untitledName, rowId: snapshot.row.id,
    dataSourceId: snapshot.row.dataSourceId, templates: [],
    rowName: databaseKindRowName({ taskDatabase: snapshot.taskDatabase }) || untitledPageDisplayTitle("page") });
}
