import { useSyncExternalStore } from "react";
import { LeafenApiError } from "@/apiTransport";
import { mutateDatabaseToolbarConfig, readDatabaseToolbarConfig } from "@/databaseToolbarConfigApi";
import {
  databaseToolbarConfigErrorCodes,
  type DatabaseToolbarConfigMetadataResponse,
  type DatabaseToolbarTemplateMetadataWire,
} from "@/databaseToolbarConfigWire.generated";
import type { Session } from "@/types";
import { newId } from "./ids";
import { localFirstProductCache } from "./localFirstProductCache";
import { createRetryPacing } from "./retryPacing";
import { onServerAnswersAgain } from "./serverAnswers";
import { serverIdentity } from "./serverOrigin";
import { isServerUnreachable } from "./serverReach";
import { templateMade } from "./templatesMadeHere";

/**
 * What a database template's editor writes — its title, values and icon —
 * is kept on this device as it is written and goes to the server behind,
 * as any other edit does (user decision 2026-11-12: a press answers on the
 * next frame): kept only in the editor until it closed, a title typed and
 * values chosen went when the editor was left open through a reload or
 * another address (2026-11-14). A change the server hasn't answered is kept
 * here through a reload, a closed tab or a server out of reach, and sent
 * once it answers again (sendKeptTemplateDrafts); the menus show it
 * meanwhile (useTemplateDraftsShown). Changes go one after another for a
 * template, each the whole of what the editor holds, by the revision the
 * server last gave; refused for one it moved past, the template is read
 * again and the change sent once more.
 */

export type TemplateDraftPatch = Pick<DatabaseToolbarTemplateMetadataWire, "icon" | "name" | "title" | "properties"> & {
  propertySpans: NonNullable<DatabaseToolbarTemplateMetadataWire["propertySpans"]>;
};
type TemplateDraft = {
  templateId: string;
  /** The page holding the database and its data source, whose settings hold the template. */
  pageId: string;
  dataSourceId: string;
  patch: TemplateDraftPatch;
  keptAt: number;
};
type KeptTemplateDrafts = { version: 1; serverKey: string; drafts: TemplateDraft[] };
/** A change shown over the settings as read: until it is sent, or until they hold `revision`. */
export type TemplateDraftShown = { templateId: string; patch: TemplateDraftPatch; revision?: number };

const SEND_AFTER_MS = 400;
const NO_DRAFTS: readonly TemplateDraftShown[] = [];

const drafts = new Map<string, TemplateDraft>();
// Sent, shown until the settings are read again past them.
const sent = new Map<string, TemplateDraftShown & { dataSourceId: string }>();
// The revision each template's last answer gave, so the next change needs no read first.
const revisions = new Map<string, number>();
const turns = new Map<string, Promise<void>>();
const timers = new Map<string, number>();
const refused = new Map<string, (error: unknown) => void>();
const listeners = new Set<() => void>();
const sentListeners = new Set<(dataSourceId: string) => void>();
let latest: Session | null = null;
let clock = 0;

const keyOf = (userId: string, templateId: string) => `${userId}\u0000${templateId}`;
const shownCache = new Map<string, readonly TemplateDraftShown[]>();

function changed() {
  shownCache.clear();
  for (const listener of [...listeners]) listener();
}

/** The whole of what a template's editor holds, as one change sends it. */
export function templateDraftPatch(template: DatabaseToolbarTemplateMetadataWire): TemplateDraftPatch {
  return { icon: template.icon, name: template.name, title: template.title, properties: template.properties,
    propertySpans: template.propertySpans ?? {} };
}

const samePatch = (template: DatabaseToolbarTemplateMetadataWire, patch: TemplateDraftPatch) => (
  JSON.stringify(templateDraftPatch(template)) === JSON.stringify(patch)
);

// The person's kept changes go down in one record, one write at a time,
// the last one again when changes came while one was written.
let writing: Promise<void> | null = null;
let writeAgain = false;
function persist(session: Session) {
  if (writing) {
    writeAgain = true;
    return;
  }
  const userId = session.user.id;
  const record: KeptTemplateDrafts = { version: 1, serverKey: serverIdentity(),
    drafts: [...drafts.entries()].filter(([key]) => key.startsWith(`${userId}\u0000`)).map(([, draft]) => draft) };
  writing = localFirstProductCache.templateDrafts.keep(userId, record).catch(() => undefined).finally(() => {
    writing = null;
    if (writeAgain) {
      writeAgain = false;
      persist(session);
    }
  });
}

async function readKept(session: Session): Promise<TemplateDraft[]> {
  const stored = await localFirstProductCache.templateDrafts.read(session.user.id).catch(() => null) as KeptTemplateDrafts | null;
  if (!stored || stored.version !== 1 || stored.serverKey !== serverIdentity() || !Array.isArray(stored.drafts)) return [];
  return stored.drafts.filter((draft) => typeof draft?.templateId === "string" && typeof draft.pageId === "string"
    && typeof draft.dataSourceId === "string" && draft.patch && typeof draft.patch === "object");
}

function drop(session: Session, key: string, keptAt: number) {
  if (drafts.get(key)?.keptAt !== keptAt) return;
  drafts.delete(key);
  persist(session);
}

async function templateNow(session: Session, draft: TemplateDraft) {
  let config: DatabaseToolbarConfigMetadataResponse = await readDatabaseToolbarConfig(session, draft.pageId, draft.dataSourceId);
  let template = config.templates.find(({ id }) => id === draft.templateId);
  for (let pages = 0; !template && config.hasMore && config.nextCursor && pages < 20; pages += 1) {
    config = await readDatabaseToolbarConfig(session, draft.pageId, draft.dataSourceId, { cursor: config.nextCursor });
    template = config.templates.find(({ id }) => id === draft.templateId);
  }
  return template;
}

const gone = (error: unknown) => error instanceof LeafenApiError && (error.status === 403 || error.status === 404
  || error.code === databaseToolbarConfigErrorCodes.notFound);
const conflict = (error: unknown) => error instanceof LeafenApiError && error.code === databaseToolbarConfigErrorCodes.conflict;

// Sends the template's change as it is now, again while a newer one came meanwhile.
async function sendTurn(session: Session, key: string) {
  for (;;) {
    const draft = drafts.get(key);
    if (!draft) return;
    await templateMade(draft.templateId);
    let revision = revisions.get(key);
    let done = false;
    for (let tries = 3; tries > 0 && !done; tries -= 1) {
      try {
        if (revision === undefined) {
          const template = await templateNow(session, draft);
          // Gone, or in the trash: nothing to write it to.
          if (!template || template.deleted) {
            drop(session, key, draft.keptAt);
            changed();
            return;
          }
          if (samePatch(template, draft.patch)) {
            revisions.set(key, template.templateAuthorityRevision);
            done = true;
            break;
          }
          revision = template.templateAuthorityRevision;
        }
        const answer = await mutateDatabaseToolbarConfig(session, draft.pageId, draft.dataSourceId, {
          requestId: newId(), expectedWorkspaceContentRevision: 0,
          operations: [{ kind: "updateTemplate", templateId: draft.templateId, expectedRevision: revision, ...draft.patch }],
        });
        revisions.set(key, answer.templateAuthorityRevision);
        sent.set(key, { templateId: draft.templateId, dataSourceId: draft.dataSourceId, patch: draft.patch,
          revision: answer.templateAuthorityRevision });
        done = true;
      } catch (error) {
        revisions.delete(key);
        revision = undefined;
        if (conflict(error) && tries > 1) continue;
        // Out of reach, it waits here to go once the server answers.
        if (isServerUnreachable(error)) {
          pacing.waited();
          return;
        }
        drop(session, key, draft.keptAt);
        changed();
        if (!gone(error)) refused.get(key)?.(error);
        return;
      }
    }
    pacing.answered();
    drop(session, key, draft.keptAt);
    changed();
    for (const listener of [...sentListeners]) listener(draft.dataSourceId);
    if (drafts.get(key) === undefined) return;
  }
}

function send(session: Session, key: string) {
  const timer = timers.get(key);
  if (timer !== undefined) window.clearTimeout(timer);
  timers.delete(key);
  const running = turns.get(key);
  const turn = (running ?? Promise.resolve()).then(() => sendTurn(session, key)).catch(() => undefined).finally(() => {
    if (turns.get(key) === turn) turns.delete(key);
  });
  turns.set(key, turn);
  return turn;
}

// Sent again as the server answers again, no sooner than the pause after a
// send that found it out of reach (retryPacing.ts).
const pacing = createRetryPacing();
let hearing = false;
function hearServerAgain() {
  if (hearing) return;
  hearing = true;
  onServerAnswersAgain(() => {
    pacing.request(() => { if (latest) sendKeptTemplateDrafts(latest); });
  });
}

/**
 * Keeps what a template's editor holds as its change, sent shortly after the
 * last keystroke; `revision` is the template's as the editor read it, and
 * `onRefused` hears a change the server won't take. A template made here and
 * not yet made by the server holds none (0, its revisions start at 1): its
 * revision is read once it is made (templatesMadeHere.ts), as one never read.
 */
export function keepTemplateDraft(session: Session, input: {
  templateId: string; pageId: string; dataSourceId: string; patch: TemplateDraftPatch; revision?: number;
  onRefused?(error: unknown): void;
}) {
  latest = session;
  hearServerAgain();
  const key = keyOf(session.user.id, input.templateId);
  clock = Math.max(clock + 1, Date.now());
  drafts.set(key, { templateId: input.templateId, pageId: input.pageId, dataSourceId: input.dataSourceId, patch: input.patch, keptAt: clock });
  if (input.revision !== undefined && input.revision > 0 && !turns.has(key) && !revisions.has(key)) revisions.set(key, input.revision);
  if (input.onRefused) refused.set(key, input.onRefused);
  changed();
  persist(session);
  const timer = timers.get(key);
  if (timer !== undefined) window.clearTimeout(timer);
  timers.set(key, window.setTimeout(() => void send(session, key), SEND_AFTER_MS));
}

/** Sends the template's kept change now, as its editor closes. */
export function sendTemplateDraftNow(session: Session, templateId: string) {
  const key = keyOf(session.user.id, templateId);
  refused.delete(key);
  if (timers.has(key)) void send(session, key);
}

/** The change kept here for a template, from an earlier tab too, or none. */
export async function keptTemplateDraft(session: Session, templateId: string): Promise<TemplateDraftPatch | null> {
  const key = keyOf(session.user.id, templateId);
  const here = drafts.get(key);
  if (here) return here.patch;
  return (await readKept(session)).find((draft) => draft.templateId === templateId)?.patch ?? null;
}

/**
 * Sends the template changes kept here the server may lack: those a tab
 * reloaded or closed left, and those kept while it was out of reach.
 */
export function sendKeptTemplateDrafts(session: Session) {
  latest = session;
  hearServerAgain();
  void readKept(session).then((kept) => {
    for (const draft of kept) {
      const key = keyOf(session.user.id, draft.templateId);
      if (!drafts.has(key)) drafts.set(key, draft);
    }
    if (kept.length) changed();
    for (const key of drafts.keys()) {
      if (key.startsWith(`${session.user.id}\u0000`) && !turns.has(key) && !timers.has(key)) void send(session, key);
    }
  });
}

/** Hears a template's change of the data source answered by the server, so its settings are read again. */
export function onTemplateDraftSent(dataSourceId: string, listener: () => void) {
  const heard = (id: string) => { if (id === dataSourceId) listener(); };
  sentListeners.add(heard);
  return () => { sentListeners.delete(heard); };
}

/** The data source's template changes to show over its settings as read. */
export function useTemplateDraftsShown(session: Session | null, dataSourceId: string): readonly TemplateDraftShown[] {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  }, () => {
    if (!session) return NO_DRAFTS;
    const cacheKey = `${session.user.id}\u0000${dataSourceId}`;
    const cached = shownCache.get(cacheKey);
    if (cached) return cached;
    const prefix = `${session.user.id}\u0000`;
    const shown: TemplateDraftShown[] = [];
    for (const [key, { dataSourceId: source, ...entry }] of sent) {
      if (key.startsWith(prefix) && source === dataSourceId && !drafts.has(key)) shown.push(entry);
    }
    for (const [key, draft] of drafts) {
      if (key.startsWith(prefix) && draft.dataSourceId === dataSourceId) shown.push({ templateId: draft.templateId, patch: draft.patch });
    }
    const value = shown.length ? shown : NO_DRAFTS;
    shownCache.set(cacheKey, value);
    return value;
  });
}

/** The templates as read with the changes shown over them; a sent one only until they hold its revision. */
export function withTemplateDrafts<Template extends DatabaseToolbarTemplateMetadataWire>(
  templates: readonly Template[], shown: readonly TemplateDraftShown[],
): Template[] {
  if (!shown.length) return templates as Template[];
  const byId = new Map(shown.map((entry) => [entry.templateId, entry]));
  return templates.map((template) => {
    const entry = byId.get(template.id);
    if (!entry || (entry.revision !== undefined && template.templateAuthorityRevision >= entry.revision)) return template;
    return { ...template, ...entry.patch };
  });
}
