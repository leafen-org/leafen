import { newId } from "./ids";
import type { LeavingChangesStore } from "./nativePageMutationLeaving";
import { pageMetadataBaseline, type PageMetadataBaseline } from "./nativePageMetadata";
import { openTypedDrafts, type TypedDraft, type TypedDrafts, type TypedValue } from "./typedDrafts";
import { afterNextPaint } from "./afterPress";
import { endsTyping, holdUntilTypingEnds, TYPING_PAUSE_MS } from "./typedWrites";
import { graphOperationSubject, type GraphOperation } from "@/contentGraph";
import type { Page } from "@/types";

/*
 * The one way a typed title-like value goes out: each key keeps the value on
 * this device at once (typedDrafts.ts), and the field sends it once — when
 * the typing pauses for TYPING_PAUSE_MS, or on the next frame when it ends
 * otherwise (Enter, Escape, Tab, the field left, ⌘Z), the page hidden or going
 * (typedWrites.ts holdUntilTypingEnds) — into the outbox as any change it
 * makes, under the ID its record holds (typedField). When Leafen opens again on this
 * device, what a tab gone kept goes into the outbox before it sends what it
 * holds (recoverTypedDrafts): under that same ID, so a value that reached the
 * server already is taken once, not written again over what came after it.
 * The fields wired here: a page's title in its head and a page renamed from
 * the sidebar, the path or ⌘⇧R; a row's page's title wherever it opens (its
 * field, richTextCellEngine.ts) and a row's value typed into its table's
 * cell or its page (rowValuesTyped.ts), which go as the row's values do
 * (rowValuesKept.ts); and a name — a database's, a property's and its
 * description, an option's, a view's — typed in its field, kept as the
 * change its field saves it as (typedName.ts), and a page's content changes
 * collected as typed (nativeContentGraphWriteCoordinator.ts), which go as a
 * page's content changes do (typedGraphChanges.ts).
 */

let current: { userId: string; drafts: TypedDrafts } | null = null;
/** What this tab keeps typed for its account, while it keeps any. */
export const typedDraftsKept = () => current?.drafts ?? null;
// The fields with typing not sent yet, which a discard stops.
const waiting = new Set<{ stop(): void }>();

/** The account whose typing this tab keeps, from its session's opening; gives what stops it. */
export function keepTypedDrafts(userId: string): () => void {
  const held = { userId, drafts: openTypedDrafts(userId) };
  current = held;
  return () => { if (current === held) current = null; };
}

/** The record of a page's title. */
export const pageTitleRecord = (pageId: string) => `page:${pageId}:title`;
/** The record of a row's value. */
export const rowValueRecord = (rowId: string, propertyId: string) => `row:${rowId}:${propertyId}`;

/**
 * What a page's title typed on `typedOn` goes out with: the page as it
 * stands, with the title it was typed on (and the cursor the server held it
 * at, when known), as the page's menus send it (usePageMenuActions).
 */
export function pageTitleDraftBase(page: Page, typedOn: Pick<PageMetadataBaseline, "title"> & Partial<Pick<PageMetadataBaseline, "cursor" | "titleSpans">>):
  PageMetadataBaseline {
  return { ...pageMetadataBaseline(page), ...typedOn };
}

/** How a rename box keeps what it types (RenameTitleField): its record, and what it goes out with, typed on a title. */
export type TypedKept = Pick<TypedDraft, "recordKey" | "kind" | "hostPageId" | "row"> & {
  base(typedOn: TypedValue): unknown;
  /** A name its owner writes as a change of a page's content (sendContentGraphChange): that change, for what is typed. */
  operations?(typed: TypedValue): GraphOperation[] | null;
};

/**
 * A row's page's name typed in a rename box, kept as its title field keeps
 * it, written as typed through the row's page's route (sendRowValues) with
 * the page holding its database (`hostPageId`).
 */
export function rowTitleKept(rowId: string, propertyId: string, hostPageId: string): TypedKept {
  return { recordKey: rowValueRecord(rowId, propertyId), kind: "rowValue", hostPageId, row: { rowId, propertyId, type: "title" }, base: () => null };
}

/** A page's name typed in a rename box, kept as its title field keeps it; `page` reads the page as it stands. */
export function pageTitleKept(pageId: string, page: () => Page | undefined): TypedKept {
  return { recordKey: pageTitleRecord(pageId), kind: "pageTitle", hostPageId: pageId, base: (on) => {
    const now = page();
    return now && pageTitleDraftBase(now, { title: on.title, ...(on.spans?.length ? { titleSpans: [...on.spans] } : {}) });
  } };
}

// The kept record whose field's send is making its change now, and the answers of the writes that make it;
// a page's content change, the records it writes (`subjects`).
let sendingNow: { recordKey: string; mutationId: string; answers: Promise<unknown>[]; subjects?: ReadonlySet<string> } | null = null;

/** The ID a page's title change made now goes under: its kept record's, while that record's field sends it (TypedField.send). */
export function typedChangeIdFor(pageId: string): string | undefined {
  return sendingNow?.recordKey === pageTitleRecord(pageId) ? sendingNow.mutationId : undefined;
}

/**
 * The change a row's value written now goes as, while its kept record's
 * field sends it (TypedField.send): its record's ID, and `went`, given the
 * write's answer, which the record waits for before it goes.
 */
export function typedRowChange(rowId: string, propertyId: string): { mutationId: string; went(answer: Promise<unknown>): void } | undefined {
  const now = sendingNow;
  if (now?.recordKey !== rowValueRecord(rowId, propertyId)) return undefined;
  // One write takes the ID: a second the same send makes is a change of its own.
  sendingNow = { ...now, recordKey: "" };
  return { mutationId: now.mutationId, went: (answer) => { now.answers.push(answer); } };
}

/**
 * The change a page's content written now goes as, while its kept record's
 * field sends it (TypedField.send) and it writes what that record does — a
 * name typed in its field (typedName.ts): its record and ID, and `went`,
 * given what keeps the change on this device (the outbox's keeping, or its
 * answer), which the record waits for before it goes.
 */
export function typedGraphChange(operations: readonly GraphOperation[]): TypedGraphClaim | undefined {
  const now = sendingNow, subjects = now?.subjects;
  if (!now || !subjects || !operations.some((operation) => subjects.has(graphOperationSubject(operation)))) return undefined;
  // One write takes the ID: a second the same send makes is a change of its own.
  sendingNow = { ...now, recordKey: "", subjects: undefined };
  // The record waits for it, though the change goes later than the send that made it (after another page's).
  let went: (answer: Promise<unknown>) => void = () => undefined;
  now.answers.push(new Promise<unknown>((resolve) => { went = (answer) => { went = () => undefined; resolve(answer.catch(() => undefined)); }; }));
  return { recordKey: now.recordKey, mutationId: now.mutationId, went: (answer) => went(answer) };
}
export type TypedGraphClaim = { recordKey: string; mutationId: string; went(answer: Promise<unknown>): void };

export type TypedField = ReturnType<typeof typedField>;

/**
 * A field's typing: each key keeps its value on this device (`key`), and the
 * field sends it (`send`) once the typing pauses or ends. Each field has its
 * own — a page's title in its head and in a rename box write the same record,
 * the newer key kept, and neither's send or stop touches the other's typing.
 */
export function typedField() {
  let pending: { recordKey: string; mutationId: string; turn: number; timer: number; release: (() => void) | null;
    send: () => void; subjects?: ReadonlySet<string> } | null = null;
  const stopHold = () => {
    if (!pending) return;
    window.clearTimeout(pending.timer);
    pending.release?.();
    pending.release = null;
  };
  const self = { stop: stopHold };
  const fire = () => {
    stopHold();
    pending?.send();
  };
  /**
   * The field sent its value (or found nothing new to send): its next key
   * begins a change of its own, and what it kept goes once `answer` settles
   * — the change answered, kept in the outbox for later, or refused — unless
   * it was typed again since.
   */
  const sent = (answer?: unknown) => {
    const was = pending;
    if (!was) return;
    stopHold();
    pending = null;
    waiting.delete(self);
    const held = current;
    if (!held || !was.turn) return;
    void Promise.resolve(answer).catch(() => undefined).then(() => held.drafts.settle(was.recordKey, was.turn)).catch(() => undefined);
  };
  // The value typed now, kept on this device under the change it goes as.
  const keep = (draft: Omit<TypedDraft, "mutationId">, send: () => void) => {
    if (pending && pending.recordKey !== draft.recordKey) sent();
    pending ??= { recordKey: draft.recordKey, mutationId: newId(), turn: 0, timer: 0, release: null, send };
    pending.send = send;
    pending.subjects = draft.operations ? new Set((draft.operations as GraphOperation[]).map(graphOperationSubject)) : undefined;
    waiting.add(self);
    if (current) pending.turn = current.drafts.keep({ ...draft, mutationId: pending.mutationId });
    return pending;
  };
  return {
    /**
     * A key typed in a field its owner saves as it is left or closed (a
     * name, typedName.ts): its value is kept on this device now as the change
     * its saving makes, and goes with that (`send`) or with its tab.
     */
    record(draft: Omit<TypedDraft, "mutationId">) {
      keep(draft, () => undefined);
    },
    /**
     * A key typed: its value is kept on this device now, and `send` — the
     * field's own, through `send` below — runs once the typing pauses or ends.
     */
    key(draft: Omit<TypedDraft, "mutationId">, send: () => void) {
      keep(draft, send);
      if (!pending) return;
      window.clearTimeout(pending.timer);
      pending.timer = window.setTimeout(fire, TYPING_PAUSE_MS);
      pending.release ??= holdUntilTypingEnds(fire);
      // Typed by a key that ends the typing itself — a step of ⌘Z — it goes after that key's frame.
      const by = window.event, typing = pending;
      if (by instanceof KeyboardEvent && by.type === "keydown" && endsTyping(by)) afterNextPaint(() => { if (pending === typing && typing.release) fire(); });
    },
    /**
     * Sends what the field typed: `make` makes the change under the ID the
     * typing was kept under, given to it — a page's title through its menus'
     * write (usePageMenuActions, typedChangeIdFor), a row's value through
     * its row's (rowValuesKept.ts, typedRowChange), a name through its
     * page's content writes (nativeContentGraphWriteCoordinator.ts,
     * typedGraphChange) — and answers what that answers; what was kept goes
     * with it once that and the write answered or kept it (`sent`). A send
     * nothing typed (a ⌘Z step) goes under an ID of its own.
     */
    send<Answer>(make: (mutationId?: string) => Answer): Answer {
      const before = sendingNow, now = pending ? { recordKey: pending.recordKey, mutationId: pending.mutationId, answers: [] as Promise<unknown>[],
        ...(pending.subjects ? { subjects: pending.subjects } : {}) } : null;
      sendingNow = now;
      let answer: Answer;
      try {
        answer = make(now?.mutationId);
      } finally {
        sendingNow = before;
      }
      sent(now?.answers.length ? Promise.all([answer, ...now.answers]) : answer);
      return answer;
    },
    sent,
  };
}

/** Where what a tab gone kept typed goes as Leafen opens: the outbox, and the content changes it holds already. */
export type TypedDraftsRecovery = Pick<LeavingChangesStore, "keepGraph" | "keepMetadata" | "replay">
  & { pendingGraph?(): Promise<ReadonlyMap<string, readonly { mutationId: string }[]>> };

/**
 * Puts what a tab gone kept — closed, reloaded, its browser killed or its
 * app ended mid-word — into the outbox as it opens, before it sends what it
 * holds: each value as the change it would have been, under the same ID.
 */
export async function recoverTypedDrafts(userId: string, store: TypedDraftsRecovery) {
  const left = await openTypedDrafts(userId).takeLeft();
  // A change the outbox holds already — kept there as it went, its tab gone
  // before its record here was taken off — stands as kept: written again, its
  // words and the rest only the outbox keeps with it would go.
  const outboxHolds = left.drafts.some(({ kind }) => kind !== "pageTitle") && store.pendingGraph
    ? new Set([...(await store.pendingGraph().catch(() => new Map())).values()].flat().map(({ mutationId }) => mutationId)) : null;
  let kept = 0;
  for (const draft of left.drafts) {
    const { value, row, operations } = draft;
    if (draft.kind !== "pageTitle" && outboxHolds?.has(draft.mutationId)) continue;
    // One the outbox refuses to keep (its page or value not one it takes) goes no further.
    const keeping = draft.kind === "pageTitle" && value ? store.keepMetadata({ baseline: draft.base as PageMetadataBaseline, mutationId: draft.mutationId,
      pageId: draft.hostPageId, change: { title: value.title, ...(value.spans ? { titleSpans: [...value.spans] } : {}) } })
      : draft.kind === "rowValue" && row && value ? store.keepGraph({ mutationId: draft.mutationId, operations: [], pageId: draft.hostPageId,
        rowValues: { rowId: row.rowId, input: typedRowValuesInput(row, value, draft.base) } })
        // A page's content change — a name typed, changes collected as typed — as its page's writes keep it.
        : draft.kind === "graph" && operations?.length ? store.keepGraph({ mutationId: draft.mutationId, operations: [...operations], pageId: draft.hostPageId })
          : null;
    await keeping?.then(() => { kept += 1; }, () => undefined);
  }
  await left.taken();
  if (kept) void store.replay().catch(() => undefined);
}

/**
 * A row's value as typed, as its row's page's route takes it
 * (rowValuesKept.ts): over the value it was typed on, when that is known
 * (`base`), which a title or a text is merged with when the server holds
 * another by then.
 */
export function typedRowValuesInput(row: NonNullable<TypedDraft["row"]>, value: TypedValue, base: unknown) {
  const written = { type: row.type, value: value.title, ...(value.spans?.length ? { spans: value.spans.map((span) => ({ ...span })) } : {}) };
  const over = base && typeof base === "object" ? base as typeof written : null;
  return { values: { [row.propertyId]: written }, ...(over ? { expected: { [row.propertyId]: over } } : {}) };
}

// What else holds typing not sent yet, which a discard lets go of too (rowValuesTyped.ts).
const holders = new Set<() => void>();
/** Lets go of what `letGo` holds when what this device kept typed is discarded. */
export function onTypedDraftsDiscarded(letGo: () => void) {
  holders.add(letGo);
}

/** Forgets what this device kept typed (its changes discarded, signed out). */
export function discardTypedDrafts(userId: string) {
  for (const field of waiting) field.stop();
  waiting.clear();
  for (const letGo of holders) letGo();
  return openTypedDrafts(userId).discard();
}

/** The outbox whose discarding (signing out, the server's data restored) forgets what was kept typed too. */
export function withTypedDraftsDiscarded<Outbox extends { discardAll(): Promise<void> }>(userId: string, outbox: Outbox): Outbox {
  return { ...outbox, discardAll: () => discardTypedDrafts(userId).catch(() => undefined).then(() => outbox.discardAll()) };
}
