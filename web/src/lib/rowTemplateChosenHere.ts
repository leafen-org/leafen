import type { GraphRow } from "@/contentGraph";
import { contentGraphOutboxOf, sendContentGraphChange } from "@/lib/contentGraphSend";
import { applyDatabaseRowTemplate, ROW_FILLED_FROM_TEMPLATE_EVENT } from "@/lib/databaseTemplateRequests";
import { chosenTimeZone } from "@/lib/dateReminders";
import { newId } from "@/lib/ids";
import { whenKeptChangeWent } from "@/lib/keptChangeWent";
import type { NativeGraphMutationOutbox } from "@/lib/nativePageMutationBrowser";
import { followPageDatabaseCopies } from "@/lib/pageDatabaseCopies";
import { pageRowMadeOn, rowNotYetMade, startRowFrom, whenRowMadeThere } from "@/lib/rowMadeHere";
import type { Session } from "@/types";

/** A row as its page holds it: enough to write it again where it stands. */
export type RowChosenFor = Pick<GraphRow, "id" | "dataSourceId" | "subitemParentId" | "position"> & { containerPageId: string };

/**
 * A template chosen on the empty body of a row's page (DatabaseRowPage). A
 * row made here that the server doesn't have yet takes it through the
 * outbox, as the template it starts from does (rowMadeHere.ts): with its
 * making while that hasn't gone yet, else in a change of the row kept behind
 * its making, which names the template (rowTemplates) and which the server
 * applies as it takes it (content_graph_row_templates.go) — once, after a
 * reload, a closed tab or offline too (2026-11-25: it waited in the page's
 * memory for the row, and a reload or a closed tab lost it). Any other row
 * takes it at once (applyDatabaseRowTemplate). Settles once the server
 * applied it, its open page told; throws when it was refused.
 */
export async function chooseRowTemplate(session: Session, row: RowChosenFor, templateId: string): Promise<void> {
  if (!rowNotYetMade(row.id)) return applyDatabaseRowTemplate(session, row.id, templateId);
  const template = { templateId, timeZone: chosenTimeZone() };
  if (startRowFrom(row.id, template)) {
    if (!(await whenRowMadeThere(row.id))) throw new Error("The row was not made.");
  } else {
    const pageId = pageRowMadeOn(row.id) ?? row.containerPageId;
    const outbox = contentGraphOutboxOf(session);
    const mutationId = newId();
    // The row stands where its changes kept here put it; its values stay as
    // they are, the template filling those it leaves empty.
    const written = await rowAsKept(outbox, pageId, row);
    const delivery = await sendContentGraphChange(session, pageId, [{ kind: "upsertRow", row: written }],
      { mutationId, rowTemplates: [{ ...template, rowId: row.id }] });
    if (delivery === "kept" && outbox && !(await whenKeptChangeWent(outbox, pageId, mutationId))) {
      throw new Error("The template's change was refused.");
    }
  }
  window.dispatchEvent(new CustomEvent(ROW_FILLED_FROM_TEMPLATE_EVENT, { detail: row.id }));
  // The body's own databases come after the rest (lib/pageDatabaseCopies.ts).
  followPageDatabaseCopies(session, row.id);
}

/** Where the row stands as the last change kept for it here puts it, or as its page holds it. */
async function rowAsKept(outbox: NativeGraphMutationOutbox | undefined, pageId: string, row: RowChosenFor): Promise<GraphRow> {
  let kept: Pick<GraphRow, "dataSourceId" | "subitemParentId" | "position"> = row;
  const writes = await outbox?.pendingGraphWrites(pageId).catch(() => []) ?? [];
  for (const { operations } of writes) {
    for (const operation of operations as { kind?: string; row?: GraphRow }[]) {
      if (operation.kind === "upsertRow" && operation.row?.id === row.id) kept = operation.row;
    }
  }
  return { id: row.id, dataSourceId: kept.dataSourceId, subitemParentId: kept.subitemParentId ?? null, position: kept.position, values: {} };
}
