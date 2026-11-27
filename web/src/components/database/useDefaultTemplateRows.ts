import { useMemo, useRef } from "react";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import type { DatabaseToolbarTemplateMetadataWire } from "@/databaseToolbarConfigWire.generated";
import { databaseTemplateActions, useDatabaseTemplates } from "@/lib/databaseTemplateRequests";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import { makeRowsFromTemplate } from "@/lib/rowMadeHere";
import {
  fillRowFromTemplateAhead, heldDefaultTemplate, heldRowTemplate, madeFromTemplate, rowTemplateToMakeFrom, rowTemplateValues, rowWithTemplateIcon,
} from "@/lib/rowTemplateHere";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import type { Session } from "@/types";
import { isNonEmptyStoredValue } from "./PropertyTypeChangeConfirm";
import { newPageTemplate, viewDefaultTemplate, type NamedPageTemplate } from "./viewDefaultTemplate";

/**
 * The template a row made in place in the view starts from, as the database's
 * toolbar offers its templates: a card being made shows its icon before its
 * row has it, as Notion's board shows the task tracker's ✅ on "+ 새 작업"'s
 * card and on the next one Enter opens (seen 2026-11-08). None: the empty page.
 */
export function useNewRowTemplate(dataSourceId: string, view: Pick<GraphView, "config">) {
  const templates = useDatabaseTemplates(dataSourceId);
  return viewDefaultTemplate(templates.map((template) => ({ ...template, isDefault: template.isDefault === true })), view);
}

/**
 * Draws a row made here from one of its database's templates as the server
 * filled it, once it made it — the fill goes with the row's making
 * (madeFromTemplate, rowTemplateHere.ts) — and does not hold the row up
 * meanwhile: it opens and takes typing at once, however the network is
 * (2026-11-12: on a stalled network a new row waited for its template, its
 * page never opened and what was typed in it was lost). The server fills the
 * values and icon the row leaves empty and puts the template's body after
 * the row's own, unless the row took it copied here (`made.copied`); here
 * the row takes the values and icon it still leaves empty, so what was
 * typed meanwhile stays.
 */
export function fillRowFromTemplate(session: Session, row: GraphRow, templateId: string, publish: (filled: GraphRow) => void,
  shown: (rowId: string) => GraphRow | undefined, failed: ((error: unknown) => void) | undefined,
  made: { filled: Promise<boolean>; copied: boolean }) {
  const filling = made.filled.then(async (filled) => {
    if (!filled) return false;
    const read = await readDatabaseRowPage(session, row.id, { limit: 1 });
    const now = shown(row.id) ?? row;
    const values = { ...now.values };
    for (const [id, value] of Object.entries(read.row.values as GraphRow["values"])) {
      if (!isNonEmptyStoredValue(values[id]?.value)) values[id] = value;
    }
    const icon = now.icon ? {} : { icon: read.row.icon, iconType: read.row.iconType as GraphRow["iconType"] };
    publish({ ...now, ...icon, values });
    // The server's copy of the template's body, which its page draws next.
    return !made.copied && read.body.length > 0;
  });
  if (!made.copied) fillRowFromTemplateAhead(session, row.id, templateId, filling);
  void filling.catch((error: unknown) => failed?.(error));
}

/**
 * What a row made now starts from: its template, as held here when it is, the
 * values it is made with, and how its making names it (rowTemplateToMakeFrom):
 * a view's default, which the database's stands for once gone (`orDefault`),
 * or the database's default as this device last heard of it (`isDefault`).
 */
export type RowStart = {
  reader: Session; templateId?: string; template?: DatabaseToolbarTemplateMetadataWire; values: Record<string, unknown>;
  orDefault?: boolean; isDefault?: boolean;
};

/**
 * What a row made now starts from, given the template `named` names
 * (newPageTemplate): that one, as the toolbar (`held`) or this device holds
 * it — a view's default held here as deleted standing for its database's —
 * or, named none, its database's default as this device last heard of it,
 * else as the server names it as it makes the row (no `templateId`); and the
 * values it is made with (rowTemplateValues). None (null) is the empty page.
 */
export function rowStartFrom(reader: Session, dataSourceId: string, named: NamedPageTemplate | null, properties: readonly GraphProperty[],
  held: (templateId: string) => DatabaseToolbarTemplateMetadataWire | undefined = () => undefined): RowStart | null {
  if (!named) return null;
  const heldAs = (templateId: string) => held(templateId) ?? heldRowTemplate(reader, templateId);
  const own = named.templateId && !(named.orDefault && heldAs(named.templateId)?.deleted) ? named.templateId : undefined;
  const templateId = own ?? heldDefaultTemplate(reader, dataSourceId)?.templateId;
  const template = templateId ? heldAs(templateId) : undefined;
  return { reader, templateId, template, values: rowTemplateValues(reader, template, properties, dataSourceId),
    ...own ? { orDefault: named.orDefault === true } : { isDefault: true } };
}

/**
 * Makes a row with `make` from the template `start` names — its making names
 * it (rowTemplateToMakeFrom), so the server fills it as it makes it, whenever
 * that is — drawn with the template's icon at once and as the server filled
 * it once made (fillRowFromTemplate); `make` alone for the empty page.
 */
export async function makeRowFrom(start: RowStart | null, make: () => Promise<GraphRow | null>, publish: (filled: GraphRow) => void,
  shown: (rowId: string) => GraphRow | undefined, failed?: (error: unknown) => void): Promise<GraphRow | null> {
  const sent = start ? rowTemplateToMakeFrom(start.reader, start.templateId, start) : null;
  const row = await makeRowsFromTemplate(sent?.template, make);
  if (!row || !start || !sent) return row;
  const made = madeFromTemplate(start.reader, row.id, sent);
  const looked = rowWithTemplateIcon(row, start.template);
  if (looked !== row) publish(looked);
  fillRowFromTemplate(start.reader, looked, start.templateId ?? "", publish, shown, failed, made);
  return looked;
}

/**
 * A row made in place starts from the view's default template, as the
 * toolbar's "새로 만들기" does: Notion's table end and board column "+" do
 * (seen 2026-11-05, the row's title the template's), its row's "+" and a
 * sub-item's "+ 새 하위 항목" too (seen the same day); a form's answer does
 * not. It is made first, so it keeps its place, its group's value and its
 * dates, and comes at once with the template's icon, values and body as
 * read ahead here (rowTemplateHere.ts); the template fills what it leaves
 * empty as the server makes the row, its making naming it (makeRowFrom) —
 * before the toolbar read its templates too, as newPageTemplate names it then.
 */
export function useDefaultTemplateRows({ session, source, view, rows, properties, tableAdapter, boardAdapter, timelineAdapter }: {
  session?: Session | null;
  source: GraphDataSource;
  view: GraphView;
  /** The rows shown, which a row filled from its template is drawn over. */
  rows: readonly GraphRow[];
  /** The database's properties, which a row shows its template's values in. */
  properties: readonly GraphProperty[];
  tableAdapter?: NativeTableProductAdapter;
  boardAdapter?: NativeBoardProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
}) {
  const latest = useRef({ session, source, view, rows, properties, tableAdapter });
  latest.current = { session, source, view, rows, properties, tableAdapter };
  return useMemo(() => {
    // The template a row made now starts from: the view's default as its toolbar reads it, or as newPageTemplate names it unread.
    const startFrom = () => {
      const { session: reader, source: database, view: shown, properties: known } = latest.current;
      const actions = databaseTemplateActions(database.id);
      const named = actions?.defaultFor ? actions.defaultFor(shown) : newPageTemplate(null, shown);
      return reader ? rowStartFrom(reader, database.id, named, known, (id) => actions?.template(id)) : null;
    };
    // What the view gives the row — its group's value, its filters' — stays, as the template fills only what the row leaves empty.
    const under = (values: Record<string, unknown>, given: Readonly<Record<string, unknown>> | undefined) => ({
      ...values, ...Object.fromEntries(Object.entries(given ?? {}).filter(([, value]) => isNonEmptyStoredValue(value))),
    });
    // Seeds that name the title apart, a board's and a timeline's.
    const titledSeeds = <Seeds extends { title?: string; properties: Record<string, unknown> }>(start: RowStart | null,
      dataSourceId: string, seeds: Seeds): Seeds => {
      if (!start) return seeds;
      const title = latest.current.properties.find((property) => property.dataSourceId === dataSourceId && property.type === "title")?.id;
      const { [title ?? ""]: templateTitle, ...values } = start.values;
      return { ...seeds, title: seeds.title || (typeof templateTitle === "string" ? templateTitle : undefined), properties: under(values, seeds.properties) };
    };
    const publish = (filled: GraphRow) => void latest.current.tableAdapter?.publishCreatedRow(filled, () => true);
    const shownRow = (rowId: string) => latest.current.rows.find(({ id }) => id === rowId);
    return {
      tableAdapter: tableAdapter && {
        ...tableAdapter,
        createRow: async (...args: Parameters<NativeTableProductAdapter["createRow"]>) => {
          const start = startFrom();
          const [database, parentId, afterRowId, isCurrent, seeds, ...rest] = args;
          return makeRowFrom(start, () => tableAdapter.createRow(database, parentId, afterRowId, isCurrent,
            start ? under(start.values, seeds) : seeds, ...rest), publish, shownRow);
        },
      },
      boardAdapter: boardAdapter && {
        ...boardAdapter,
        createRow: async (...args: Parameters<NativeBoardProductAdapter["createRow"]>) => {
          const start = startFrom();
          const [database, propertyId, value, seeds, isCurrent] = args;
          return makeRowFrom(start, () => boardAdapter.createRow(database, propertyId, value, titledSeeds(start, database.id, seeds), isCurrent),
            publish, shownRow);
        },
      },
      timelineAdapter: timelineAdapter && {
        ...timelineAdapter,
        createDatedRow: async (...args: Parameters<NativeTimelineProductAdapter["createDatedRow"]>) => {
          const start = startFrom();
          // What the view draws with the row (alongside) goes on with it: left out, its bar's title
          // drew the whole timeline again after it (2026-11-15: ~20 ms at CPU ×2 inside the press).
          const [database, view, dayKey, seeds, isCurrent, ...rest] = args;
          return makeRowFrom(start, () => timelineAdapter.createDatedRow(database, view, dayKey, titledSeeds(start, database.id, seeds), isCurrent, ...rest),
            publish, shownRow);
        },
      },
    };
  }, [boardAdapter, tableAdapter, timelineAdapter]);
}
