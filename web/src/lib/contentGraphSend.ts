import { api } from "@/api";
import type { GraphMutationInput, GraphMutationReceipt, GraphOperation, GraphRowTemplateFill } from "@/contentGraph";
import { newId } from "@/lib/ids";
import type { NativeGraphMutationOutbox } from "@/lib/nativePageMutationBrowser";
import { rowsMadeBy } from "@/lib/rowMadeHere";
import { blockChangeTextOf, type BlockChangeText } from "@/lib/blockChangeText";
import type { Session } from "@/types";

// Every change to a page's content — its blocks, databases, rows and views —
// goes to the server from here, wherever it is made: the page's editor
// (useContentGraphWrites), the sidebar, a row page opened alone, a menu.

/** What became of a change: the server's receipt, kept here for a server out of reach, or sent by another tab. */
export type ContentGraphDelivery = GraphMutationReceipt | "kept" | "sent";

// The outbox of each account signed in here (AuthenticatedSession), whose
// changes wait in it for the server.
const outboxes = new Map<string, NativeGraphMutationOutbox>();

/** Lets the account's changes wait in its outbox while it is signed in here; answers the release. */
export function keepContentGraphOutbox(userId: string, outbox: NativeGraphMutationOutbox) {
  outboxes.set(userId, outbox);
  return () => { if (outboxes.get(userId) === outbox) outboxes.delete(userId); };
}

/** The outbox the session's changes wait in, if it has one. */
export function contentGraphOutboxOf(session: Pick<Session, "user">): NativeGraphMutationOutbox | undefined {
  return outboxes.get(session.user.id);
}

/**
 * Sends a change to a page's content. With the account's outbox it is kept
 * there first and goes after every older change kept: when the server can't
 * be reached it waits ("kept"), on screen where the page's copies show it,
 * and is sent once the server can be reached, as the editor's own changes
 * are; "sent" when another tab sent it. Without an outbox it goes as it is.
 * A refusal throws, and the change is gone. One making the page's database
 * makes the page a full-page database with it (`pageKind`), kept with it,
 * and, started on a page of the person's (`replacesPage`), puts the page as
 * it was in the trash (database_start_page_trash.go). A template chosen for
 * a row the change writes (`rowTemplates`, lib/rowTemplateChosenHere.ts) is
 * kept with it as the templates of the rows it makes are.
 */
export async function sendContentGraphChange(
  session: Session, pageId: string, operations: GraphOperation[],
  options: { mutationId?: string; signal?: AbortSignal; pageKind?: "database"; replacesPage?: true; rowTemplates?: GraphRowTemplateFill[] }
    & BlockChangeText = {},
): Promise<ContentGraphDelivery> {
  const mutationId = options.mutationId ?? newId();
  // Rows made here that this change makes are made once it reaches the
  // server, filled from the templates they start from, which go with it.
  const rows = rowsMadeBy(pageId, mutationId, operations);
  const outbox = contentGraphOutboxOf(session);
  const kind = options.pageKind ? { pageKind: options.pageKind, ...(options.replacesPage ? { replacesPage: options.replacesPage } : {}) } : {};
  const fills = [...rows.templates, ...options.rowTemplates ?? []];
  const templates = fills.length ? { rowTemplates: fills } : {};
  let delivery: Awaited<ReturnType<NativeGraphMutationOutbox["admitAndSendGraph"]>>;
  try {
    if (!outbox) {
      const receipt = await sendContentGraphChangeNow(session, pageId,
        { mutationId, operations, ...kind, ...blockChangeTextOf(options), ...templates }, options.signal);
      rows.reached();
      return receipt;
    }
    delivery = await outbox.admitAndSendGraph({ mutationId, operations, pageId, ...kind, ...blockChangeTextOf(options), ...templates }, options.signal);
  } catch (error) {
    rows.reached(false);
    throw error;
  }
  if (delivery.state === "kept") {
    rows.sentFrom(outbox);
    return "kept";
  }
  rows.reached();
  return delivery.state === "committed" ? delivery.result as GraphMutationReceipt : "sent";
}

/**
 * Sends a change straight to the server, not kept to wait for it: the
 * outbox's own sending; a change that also makes the page a page again,
 * names it, or is made on the page as it was read (pageKind "page",
 * pageTitle, its base cursor), which the outbox doesn't keep, and one making
 * it a database the server starts first (a form's); and a step of
 * a change in several steps that the next one needs taken by the server
 * (a database turned into a page of its own, and back).
 * A change's words — the page's text written with its blocks — and the
 * typing before it go with it, kept with it in the outbox
 * (lib/textBehindBlockChanges.ts).
 */
export function sendContentGraphChangeNow(session: Session, pageId: string, input: GraphMutationInput, signal?: AbortSignal) {
  return api.mutateContentGraph(session, pageId, input, signal);
}
