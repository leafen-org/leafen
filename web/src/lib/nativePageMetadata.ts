import { api } from "@/api";
import { changeWikiPage, readDatabaseRowPage, writeDatabaseRowPageValues } from "@/databaseRowPageApi";
import { newId } from "@/lib/ids";
import { keptChangeTaken, takenPageChange } from "@/lib/keptChangeTaken";
import { publishPageRoomMutation } from "@/lib/pageRoomEvents";
import { noteTrashMove } from "@/lib/pageTrashMoves";
import { tellPlaceEditedHere } from "@/lib/pagePlaceEdits";
import { writeRowTitle } from "@/lib/rowTitleSave";
import { type TitleValue } from "@/lib/titleMerge";
import { sendTitleOverBase } from "@/lib/typedTitleSend";
import { sendKeepingList } from "@/lib/workspaceListToken";
import type { Page, Session } from "@/types";

export type NativePageMetadataChange = Partial<Pick<
  Page,
  "cover" | "coverPosition" | "icon" | "iconType" | "inTrash" | "isLocked" | "parentId" | "position" | "title" | "titleSpans" | "font" | "smallText" | "fullWidth" | "backlinksDisplay" | "pageCommentsDisplay" | "tableOfContentsDisplay" | "inlineCommentsDisplay"
>> & {
  /** Archives or unarchives the page itself; the server records who and when. */
  isArchived?: boolean;
  /**
   * Moves the page, with the pages under it, to the top of a teamspace, or
   * with "" to the top of the mover's private pages.
   */
  teamspaceId?: string;
  /**
   * With a position alone: the page only takes it to keep an order shown (a
   * section turned to its order by hand), which is no edit of it — who
   * edited it last, and when, stay (page_metadata_kept_order.go).
   */
  keepLastEdit?: boolean;
};

/** The page as it reads once `change` is saved, before the server answers. */
export function optimisticPageMetadata(page: Page, change: NativePageMetadataChange, actorId: string): Page {
  const { isArchived, keepLastEdit: _kept, ...pageChange } = change;
  const next: Page = { ...page, ...pageChange };
  // The lock names who set it until it is unlocked (page_lock_schema.go).
  if (change.isLocked === true && !page.isLocked) next.lockedBy = actorId;
  else if (change.isLocked === false) delete next.lockedBy;
  if (isArchived === true && !page.archivedAt) {
    next.archivedAt = new Date().toISOString();
    next.archivedBy = actorId;
  } else if (isArchived === false) {
    delete next.archivedAt;
    delete next.archivedBy;
  }
  return next;
}

/**
 * What a kept page-settings change needs of the page it was made on to be
 * sent later; a wiki's page names its wiki's database, whose page it goes as
 * (sendKeptPageMetadata).
 */
export type PageMetadataBaseline = Pick<Page, "cursor" | "inTrash" | "parentId" | "title" | "titleSpans" | "wikiDataSourceId">;

export function pageMetadataBaseline(page: Page): PageMetadataBaseline {
  return { cursor: page.cursor, inTrash: page.inTrash, parentId: page.parentId, title: page.title,
    ...(page.titleSpans?.length ? { titleSpans: page.titleSpans } : {}),
    ...(isWikiDatabasePage(page) ? { wikiDataSourceId: page.wikiDataSourceId } : {}) };
}

export function isCursorConflict(error: unknown) {
  return typeof error === "object" && error !== null && Reflect.get(error, "code") === "LEAFEN_CURSOR_CONFLICT";
}

function metadataInput(page: PageMetadataBaseline, change: NativePageMetadataChange, mutationId: string,
  after?: Pick<KeptPageMetadata, "after" | "carriesAfter">) {
  // A title written with its spans sends them, none for plain words; one
  // written without keeps the page's while its words stay (page_title_spans.go).
  const written: TitleValue | null = change.title === undefined ? null : { title: change.title, spans: change.titleSpans };
  const title = written ? written.title : page.title;
  return {
    title,
    ...(written && change.titleSpans !== undefined ? { titleSpans: [...written.spans ?? []] } : {}),
    ...(change.title === undefined ? {} : { titleEdited: true }),
    parentId: change.parentId ?? page.parentId,
    ...(change.teamspaceId === undefined ? {} : { teamspaceId: change.teamspaceId }),
    inTrash: change.inTrash ?? page.inTrash,
    ...(change.position === undefined ? {} : { position: change.position, ...(change.keepLastEdit ? { keepLastEdit: true } : {}) }),
    ...(change.isLocked === undefined ? {} : { isLocked: change.isLocked }),
    ...(change.isArchived === undefined ? {} : { isArchived: change.isArchived }),
    ...(change.icon === undefined ? {} : { icon: change.icon }),
    ...(change.iconType === undefined ? {} : { iconType: change.iconType }),
    ...(change.cover === undefined ? {} : { cover: change.cover }),
    ...(change.coverPosition === undefined ? {} : { coverPosition: change.coverPosition }),
    ...(change.font === undefined ? {} : { font: change.font }),
    ...(change.smallText === undefined ? {} : { smallText: change.smallText }),
    ...(change.fullWidth === undefined ? {} : { fullWidth: change.fullWidth }),
    ...(change.backlinksDisplay === undefined ? {} : { backlinksDisplay: change.backlinksDisplay }),
    ...(change.pageCommentsDisplay === undefined ? {} : { pageCommentsDisplay: change.pageCommentsDisplay }),
    ...(change.tableOfContentsDisplay === undefined ? {} : { tableOfContentsDisplay: change.tableOfContentsDisplay }),
    ...(change.inlineCommentsDisplay === undefined ? {} : { inlineCommentsDisplay: change.inlineCommentsDisplay }),
    mutationId,
    baseCursor: page.cursor,
    ...(after?.after?.length ? { afterMutationIds: [...after.after], ...(after.carriesAfter ? { carriesAfter: true } : {}) } : {}),
  };
}

// The list the change is made on goes with it (lib/workspaceListToken.ts).
function withListToken<Input extends object>(input: Input, token: string | undefined) {
  return token ? { ...input, workspaceRefreshToken: token } : input;
}

export async function commitNativePageMetadata(
  session: Session,
  page: Page,
  change: NativePageMetadataChange,
  signal?: AbortSignal,
) {
  const input = metadataInput(page, change, newId());
  const sending = sendKeepingList((token) => api.updatePageMetadata(session, page.id, withListToken(input, token), signal));
  // A page opened while it moves to or out of the trash is read once it has (lib/pageTrashMoves.ts).
  const saved = await (change.inTrash === undefined ? sending : noteTrashMove(page.id, sending));
  announcePageMetadata(page.id);
  tellPlaceEditedHere(page, change, session.user.id);
  return saved;
}

/**
 * A page settings change made away from the page menus — a wiki's page moved
 * from the move window — kept in the outbox and sent in turn as the menus'
 * are (usePageMenuActions), sent again as the page goes (2026-11-27: a wiki's
 * page moved within its wiki and the page reloaded at once stayed where it
 * was 3 times in 4). Answers the page as saved, or as changed when it is
 * kept for a server out of reach; refused, it throws. Without an outbox it
 * goes at once.
 */
export async function keepAndSendPageMetadata(session: Session, outbox: { admitAndSendMetadata(input: KeptPageMetadata, signal?: AbortSignal):
  Promise<{ state: string; result?: unknown }> } | undefined, page: Page, change: NativePageMetadataChange, signal?: AbortSignal): Promise<Page> {
  const input: KeptPageMetadata = { baseline: pageMetadataBaseline(page), change, mutationId: newId(), pageId: page.id };
  if (!outbox) return (await sendKeptPageMetadata(session, input, signal)).page;
  const delivery = await outbox.admitAndSendMetadata(input, signal);
  return delivery.state === "committed" ? (delivery.result as Awaited<ReturnType<typeof api.updatePageMetadata>>).page
    : optimisticPageMetadata(page, change, session.user.id);
}

type KeptPageMetadata = { baseline: PageMetadataBaseline; change: NativePageMetadataChange; mutationId: string; pageId: string; after?: string[];
  carriesAfter?: true };

// The page each kept change made on a page made here went on, by its mutation.
const madeHereBaselines = new Map<string, PageMetadataBaseline>();

/**
 * A kept change made on a page made here before the server made it is on a
 * page the server never had (its cursor 0, the server's past it): it goes on
 * the page as the server now holds it, rather than be refused for its cursor
 * first. Sent again — past a lane held for the page's making and then in turn
 * — it goes on that same page, so the server takes it once (2026-11-08: a new
 * page's title was refused for its cursor, then sent again).
 */
async function keptBaseline(session: Session, input: KeptPageMetadata, signal?: AbortSignal) {
  if (input.baseline.cursor !== 0) return input.baseline;
  const known = madeHereBaselines.get(input.mutationId);
  if (known) return known;
  const read = pageMetadataBaseline(await api.readPage(session, input.pageId, signal));
  const baseline = madeHereBaselines.get(input.mutationId) ?? read;
  madeHereBaselines.set(input.mutationId, baseline);
  return baseline;
}

/**
 * Sends a page-settings change the outbox kept (nativePageMutationOutbox.ts):
 * on the page as it was when made, or — the page moved on meanwhile, on
 * another device or by a change of its own — on the page as it now is, as
 * the screen does with a change it sends itself. A title typed on an older
 * title is merged with the page's title as it now is: what was typed here
 * and what was saved elsewhere both stay, as the body's shared text keeps
 * both (2026-11-10: a window's title typed on the old one replaced
 * another's typing). A title the page no longer takes, the page having
 * become a database's page, goes as that page's (titleAsDatabasePage).
 * One the server took already, its answer lost to a reload or a closed tab
 * and built again here on the page that send made, is answered as taken
 * (keptChangeTaken.ts), not refused (2026-11-22: a new page's title saved
 * as the tab went to another page was then said to be deleted). A wiki's
 * page's goes by the wiki's page route (keptAsWikiPage).
 */
export async function sendKeptPageMetadata(
  session: Session,
  input: KeptPageMetadata,
  signal?: AbortSignal,
) {
  let baseline = await keptBaseline(session, input, signal);
  const change = input.change;
  let mutationId = input.mutationId;
  if (isWikiDatabasePage(baseline)) {
    const sent = await keptAsWikiPage(session, input.pageId, baseline, change, mutationId, signal);
    if ("receipt" in sent) return sent;
    // Out of its wiki meanwhile, it goes as the page it now is.
    baseline = sent;
    mutationId = newId();
  }
  // What the last try sent, which a refusal of it is answered with (titleAsDatabasePage).
  let sent = { baseline, change, mutationId };
  const typedOn: TitleValue = { title: baseline.title, spans: baseline.titleSpans };
  try {
    // A title goes by the one rule a typed title goes by (typedTitleSend.ts); any other change on the page as it now is.
    return await sendTitleOverBase({
      base: typedOn, typed: change.title === undefined ? typedOn : { title: change.title, spans: change.titleSpans },
      readNow: () => api.readPage(session, input.pageId, signal), titleOf: (page: Page) => ({ title: page.title, spans: page.titleSpans }),
      changed: isCursorConflict,
      async write(title, now) {
        if (now) {
          sent = { baseline: pageMetadataBaseline(now), mutationId: newId(), change: change.title === undefined ? change
            : { ...change, title: title.title, ...(change.titleSpans === undefined ? {} : { titleSpans: [...title.spans ?? []] }) } };
        }
        try {
          const sending = metadataInput(sent.baseline, sent.change, sent.mutationId, input);
          const saved = await sendKeepingList((token) => api.updatePageMetadata(session, input.pageId, withListToken(sending, token), signal));
          announcePageMetadata(input.pageId);
          // The pages its place change edits show it (lib/pagePlaceEdits.ts).
          tellPlaceEditedHere(sent.baseline, sent.change, session.user.id);
          return saved;
        } catch (error) {
          // Taken already, its answer lost — a new document builds it again on
          // the page its first send made (keptChangeTaken.ts).
          if (!keptChangeTaken(error)) throw error;
          const taken = await takenPageChange(session, input.pageId, sent.mutationId, signal);
          announcePageMetadata(input.pageId);
          return taken;
        }
      },
    });
  } catch (error) {
    const resent = await titleAsDatabasePage(session, input.pageId, sent.baseline, sent.change, sent.mutationId, error, signal);
    if (resent) return resent;
    throw error;
  }
}

/**
 * A title change kept for a page that became a database's page before it went —
 * moved into a database or a wiki elsewhere, or while it waited for the
 * server — is refused by the page's road, a database's page taking its title
 * as its row's: it goes as that page's title, merged with the title it holds
 * now as a row's is (rowTitleSave.ts), as the screen sends a wiki's page's
 * (lib/pageKindChange.ts). Answers as the page's road would; undefined for
 * any other refusal, which stands. It throws the refusal of the title so sent.
 */
async function titleAsDatabasePage(session: Session, pageId: string, baseline: PageMetadataBaseline, change: NativePageMetadataChange,
  mutationId: string, refusal: unknown, signal?: AbortSignal): Promise<Awaited<ReturnType<typeof api.updatePageMetadata>> | undefined> {
  const titleOnly = Object.keys(change).every((key) => key === "title" || key === "titleSpans");
  if (change.title === undefined || !titleOnly || Reflect.get(Object(refusal), "status") !== 400) return undefined;
  const now = await api.readPage(session, pageId, signal).catch(() => null);
  if (!now || !(isWikiDatabasePage(now) || now.databaseRow)) return undefined;
  const typed = { title: change.title, titleSpans: change.titleSpans };
  if (isWikiDatabasePage(now)) await commitWikiPageMetadata(session, now, change, signal, baseline);
  else {
    const { properties } = await readDatabaseRowPage(session, pageId, { limit: 1, signal });
    const property = properties.find(({ type }) => type === "title")?.id;
    if (!property) return undefined;
    const value = (title: TitleValue) => ({ type: "title", value: title.title, ...(title.spans?.length ? { spans: title.spans.map((span) => ({ ...span })) } : {}) });
    await writeRowTitle(session, pageId, { title: typed.title, spans: typed.titleSpans }, { title: baseline.title, spans: baseline.titleSpans },
      (title, read) => writeDatabaseRowPageValues(session, pageId, {
        mutationId: newId(), values: { [property]: value(title) }, ...(read ? { expected: { [property]: value(read) } } : {}),
      }, signal));
    announcePageMetadata(pageId);
  }
  const page = await api.readPage(session, pageId, signal);
  return { page, receipt: { mutationId, cursor: page.cursor, duplicate: false } };
}

/**
 * Sends a kept change of a wiki's page — its title, icon, place in the wiki,
 * trash — as its menu would (commitWikiPageMetadata), under the kept
 * change's ID: sent again as the page goes and once more from the outbox
 * after a reload, the server takes it once (2026-11-27: sent straight from
 * the window, a wiki's page renamed in the sidebar and reloaded or closed at
 * once lost its name 2 times in 3). Answers as the page's route would; one
 * no longer a wiki's page, moved out elsewhere, answers the page as it now
 * is, which the change then goes on as.
 */
async function keptAsWikiPage(session: Session, pageId: string, baseline: PageMetadataBaseline, change: NativePageMetadataChange,
  mutationId: string, signal?: AbortSignal): Promise<Awaited<ReturnType<typeof api.updatePageMetadata>> | PageMetadataBaseline> {
  let duplicate = false;
  try {
    duplicate = (await commitWikiPageMetadata(session, { ...baseline, id: pageId }, change, signal, baseline, mutationId)).duplicate;
  } catch (error) {
    if (Reflect.get(Object(error), "status") !== 404) throw error;
    const now = await api.readPage(session, pageId, signal).catch(() => null);
    if (!now || isWikiDatabasePage(now)) throw error;
    return pageMetadataBaseline(now);
  }
  const page = await api.readPage(session, pageId, signal);
  return { page, receipt: { mutationId, cursor: page.cursor, duplicate } };
}

// The others with the page open read its title, icon and style again at once
// instead of at their next check (2026-10-13: a title showed 7 to 10 seconds
// late); they read what the server holds, not what this says.
function announcePageMetadata(pageId: string) {
  publishPageRoomMutation({ kind: "page_meta_changed", pageId });
}

/** A page of a wiki whose pages are its database's pages. */
export function isWikiDatabasePage(page: Pick<Page, "isWiki" | "wikiDataSourceId">) {
  return Boolean(page.wikiDataSourceId) && !page.isWiki;
}

/**
 * Saves a page menu change to a wiki's page as the wiki database's page:
 * its title, icon, place in the wiki and trash. Its page style stays. A
 * title typed on the page's title (`typedOn`, else the page's as given) is
 * merged with the title the server holds then, and written only over that
 * one, as a row's title is (rowTitleSave.ts); it keeps its mentions and marks.
 * Its first write goes under `requestId` when given (a kept change's ID,
 * keptAsWikiPage), a write after it under a new one.
 */
export async function commitWikiPageMetadata(
  session: Session,
  page: Pick<Page, "id" | "parentId" | "title"> & Partial<Pick<Page, "icon" | "iconType" | "titleSpans">>,
  change: NativePageMetadataChange,
  signal?: AbortSignal,
  typedOn?: Pick<Page, "title" | "titleSpans">,
  requestId?: string,
) {
  const iconChanged = change.icon !== undefined || change.iconType !== undefined;
  const base = typedOn ?? page;
  let firstId = requestId;
  const send = (title?: TitleValue, read?: TitleValue | null) => {
    signal?.throwIfAborted();
    const id = firstId ?? newId();
    firstId = undefined;
    return changeWikiPage(session, page.id, {
      requestId: id,
      ...(title === undefined ? {} : { title: title.title, ...(title.spans?.length ? { titleSpans: title.spans.map((span) => ({ ...span })) } : {}) }),
      ...(read ? { expectedTitle: read.title } : {}),
      ...(iconChanged ? { icon: change.icon ?? page.icon ?? "", iconType: change.iconType ?? page.iconType ?? "none" } : {}),
      ...(change.parentId === undefined || change.parentId === page.parentId ? {} : { parentId: change.parentId }),
      ...(change.position === undefined ? {} : { position: change.position }),
      ...(change.inTrash === undefined ? {} : { inTrash: change.inTrash }),
    }, signal);
  };
  // A title given as words alone keeps the marks the page's title has on them.
  const typed: TitleValue | undefined = change.title === undefined ? undefined : { title: change.title,
    spans: change.titleSpans ?? (change.title === base.title ? base.titleSpans : undefined) };
  const saved = typed === undefined ? await send()
    : await writeRowTitle(session, page.id, typed, { title: base.title, spans: base.titleSpans }, send);
  announcePageMetadata(page.id);
  return saved;
}
