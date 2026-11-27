import type { ContentGraph, GraphOperation, GraphRow } from "@/contentGraph";
import { updatePersonAbout } from "@/lib/peopleProfiles";
import { heldTypedWrites } from "@/lib/typedWrites";
import type { Session } from "@/types";

// People's own five values, written the same from every view of People —
// its own page, a linked view on any page, a dashboard's widget, a row's
// page, a phone's (databaseSourceRules.tsx decides what else People's views
// allow). Its read gives each row its person's Email and names the five the
// reader doesn't change ("readOnlyValues", people_database_rules.go): Name,
// Email, Membership Type and Person are its people's, kept by the server, so
// none of them is written in a row; About is the person's own, which the
// workspace's owner writes for anyone but a deactivated person, into their
// profile, not the row — the server writes the row after it
// (savePersonAbout). It shows at once, a step of ⌘Z like any value, and a
// value typed in is saved once the typing ends (lib/typedWrites.ts) — one
// save, one run of People's automations — as Notion's People lets its owner
// (seen 2026-11-15 twice in a free test workspace: a press on another's
// About cell opened its field, what was typed stayed, and ⌘Z took it back).

type PeopleCells = { fixed: ReadonlySet<string>; about: string; person: string };

/** People's own five of a data source, and which of them are About and Person; null for any other source. */
function peopleCellsOf(graph: Pick<ContentGraph, "dataSources" | "properties">, dataSourceId: string): PeopleCells | null {
  const source = graph.dataSources.find(({ id }) => id === dataSourceId);
  if (source?.peopleDatabase !== true || !source.fixedPropertyIds?.length) return null;
  const fixed = new Set(source.fixedPropertyIds);
  const own = graph.properties.filter(({ id }) => fixed.has(id));
  return { fixed, about: own.find(({ type }) => type === "rich_text")?.id ?? "", person: own.find(({ type }) => type === "person")?.id ?? "" };
}

/** Whether a value of a row is one of People's own, which no write sets in its row (a Name's title among them). */
export function peopleOwnValue(graph: Pick<ContentGraph, "dataSources" | "properties">, row: Pick<GraphRow, "dataSourceId">, propertyId: string) {
  return peopleCellsOf(graph, row.dataSourceId)?.fixed.has(propertyId) === true;
}

/**
 * Rows' changes as People's rule lets them be written: of People's own
 * values its About alone, where the row lets its reader write it; a row left
 * with nothing to write goes.
 */
export function peopleRowsWritten(graph: ContentGraph, changes: readonly GraphRow[]): GraphRow[] {
  return changes.flatMap((change) => {
    const cells = peopleCellsOf(graph, change.dataSourceId);
    if (!cells) return [change];
    const shown = graph.rows.find(({ id }) => id === change.id);
    const aboutWritable = Boolean(cells.about) && !shown?.readOnlyValues?.includes(cells.about);
    const values = Object.fromEntries(Object.entries(change.values)
      .filter(([id]) => !cells.fixed.has(id) || (id === cells.about && aboutWritable)));
    if (!Object.keys(values).length && change.position === shown?.position) return [];
    return [{ ...change, values }];
  });
}

type PeopleAbout = { userId: string; about: string };

/**
 * Takes People's Abouts out of a write's rows, for their people's profiles:
 * what is left goes to the rows as any write.
 */
export function peopleAboutsOf(graph: ContentGraph, operations: readonly GraphOperation[]) {
  const abouts: PeopleAbout[] = [];
  const rest = operations.flatMap((operation): GraphOperation[] => {
    if (operation.kind !== "upsertRow") return [operation];
    const { row } = operation;
    const cells = peopleCellsOf(graph, row.dataSourceId);
    if (!cells?.about || !(cells.about in row.values)) return [operation];
    const shown = graph.rows.find(({ id }) => id === row.id);
    const person = shown?.values[cells.person]?.value;
    const userId = Array.isArray(person) && typeof person[0] === "string" ? person[0] : "";
    const value = row.values[cells.about]?.value;
    if (userId) abouts.push({ userId, about: typeof value === "string" ? value.trim() : "" });
    const values = Object.fromEntries(Object.entries(row.values).filter(([id]) => id !== cells.about));
    // A row left with no value or place of its own to write goes.
    if (!Object.keys(values).length && (!shown || row.position === shown.position)) return [];
    return [{ kind: "upsertRow", row: { ...row, values } }];
  });
  return { rest, abouts };
}

// One for the app: a write is made by a new adapter at each change (madeAtEachCall), the typing it holds is its person's.
const heldAbouts = heldTypedWrites();

/**
 * Saves People's Abouts into their people's profiles, held while they are
 * typed (`typed`); whether every one was saved, the latest first.
 */
export async function savePeopleAbouts(session: Session | null | undefined, workspaceId: string | null | undefined,
  abouts: readonly PeopleAbout[], typed: boolean): Promise<boolean> {
  if (!abouts.length) return true;
  if (!session || !workspaceId) return false;
  const saved = await Promise.all(abouts.map(({ userId, about }) => {
    const field = `${workspaceId}/${userId}`;
    const answer = heldAbouts.hold(field, () => updatePersonAbout(session, workspaceId, userId, about).then(() => true));
    if (!typed) heldAbouts.now(field);
    return answer;
  }));
  return saved.every(Boolean);
}

