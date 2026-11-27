import { useEffect } from "react";
import * as Y from "yjs";
import { api } from "@/api";
import {
  materializeContentGraph, type GraphBlock, type GraphProperty, type GraphReadResponse, type GraphRow,
} from "@/contentGraph";
import { graphTextBlockTypes } from "@/contentGraphWire.generated";
import type { DatabaseToolbarTemplateMetadataWire } from "@/databaseToolbarConfigWire.generated";
import { storedIconPresentation } from "@/components/page/nativeIcons";
import { isNonEmptyStoredValue } from "@/components/database/PropertyTypeChangeConfirm";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import { bytesToBase64, writeRichTextToYText, yTextToRichText } from "@/lib/collaborationCrdtCodec";
import { noteRowTakingTemplate, ROW_FILLED_FROM_TEMPLATE_EVENT } from "@/lib/databaseTemplateRequests";
import { chosenTimeZone } from "@/lib/dateReminders";
import { newId } from "@/lib/ids";
import { followPageDatabaseCopies } from "@/lib/pageDatabaseCopies";
import { keepRowBodyMadeHere, whenRowMadeThere, type RowMadeFromTemplate } from "@/lib/rowMadeHere";
import { setPageFilling } from "@/lib/pageCopyProgress";
import type { TextSpan } from "@/lib/types";
import type { Session } from "@/types";

// A row made from a template shows the template's values, icon and body as
// its page opens, as Notion's (2026-11-12: the body came some 1.7 seconds
// later, once the server had the row and had applied the template to it, and
// never on a stalled network). The templates a database's toolbar offers are
// read ahead here, their bodies with their words; a row made here from one
// takes a copy of that body under new IDs, kept as its page's copy here and
// sent with the change that makes the row, its words with it, which the
// server writes as the row's body as it makes it, filling the values and look
// the row leaves empty from the template (content_graph_row_templates.go;
// rowTemplateToMakeFrom). As the server's copy for the app
// does (database_row_page_body_copy.go): a synced block original comes as a
// copy of it, its blocks left out (the decision's 3A for the app's template
// use), and template mentions take their value for this use. A body this
// device can't copy as the server would — a database or a sub-page in it,
// which the server copies after the rest (database_row_page_sub_page_copies.go),
// more than one read's window, not read yet — goes the server's way, as before.

/** A template's body as read ahead: its blocks, and its words in a document of their own. */
type TemplateBody = { templateId: string; blocks: GraphBlock[]; words: Y.Doc; at: number };

type Held = {
  reading: boolean; at: number; body: TemplateBody | null; template?: DatabaseToolbarTemplateMetadataWire;
  /** Being changed here (dropRowTemplateAhead): not read ahead till it is left (readRowTemplateAfterChange). */
  changing?: boolean;
  /** Whether its body holds a database of its own, which the server copies into a row after the rest (lib/pageDatabaseCopies.ts). */
  ownDatabases?: boolean;
};

const held = new Map<string, Held>();
// A template read this recently isn't read again.
const READ_AGAIN_MS = 1_000;
// A copy older than this isn't used: the server copies the template instead.
const KEPT_MS = 15 * 60_000;
// The templates of a database read ahead at most.
const AHEAD_PER_DATABASE = 5;
// The default template a data source's toolbar last named, by data source.
const defaults = new Map<string, string>();

function heldKey(session: Pick<Session, "user">, templateId: string) {
  return `${session.user.id}\u0000${templateId}`;
}

function base64Bytes(value: string) {
  return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
}

/** Whether a block can be copied here as the server would. */
function copiedHere(block: GraphBlock) {
  return block.type !== "inline_database" && block.type !== "child_database" && block.type !== "child_page";
}

function templateBody(templateId: string, read: GraphReadResponse): TemplateBody | null {
  if (read.hasMore || read.nextCursor) return null;
  const { blocks } = materializeContentGraph(read.items);
  if (!blocks.length || !blocks.every(copiedHere)) return null;
  const words = new Y.Doc();
  if (read.words) Y.applyUpdate(words, base64Bytes(read.words));
  return { templateId, blocks, words, at: Date.now() };
}

/**
 * Reads a template's body ahead, so a row made from it shows it at once;
 * one read a moment ago is not read again. The copy read before stays in
 * use until the new one comes.
 */
export function readRowTemplateAhead(session: Session, templateId: string, template?: DatabaseToolbarTemplateMetadataWire) {
  const key = heldKey(session, templateId);
  const kept = held.get(key);
  if (kept) {
    if (template) kept.template = template;
    if (kept.changing || kept.reading || Date.now() - kept.at < READ_AGAIN_MS) return;
  }
  const entry: Held = { reading: true, at: Date.now(), body: kept?.body ?? null, template: template ?? kept?.template };
  held.set(key, entry);
  if (template?.hasContent === false) {
    entry.reading = false;
    entry.body = null;
    return;
  }
  // One let go meanwhile (dropRowTemplateAhead) takes nothing from it.
  void api.contentGraph(session, templateId, null, { rows: false, words: true }).then((read) => {
    entry.body = templateBody(templateId, read);
    entry.ownDatabases = materializeContentGraph(read.items).blocks.some(ownDatabase);
    entry.at = Date.now();
  }, () => { entry.body = null; }).finally(() => { entry.reading = false; });
}

/**
 * Reads ahead the templates a database's toolbar offers, its default first,
 * and remembers which one is its default for rows made elsewhere (the
 * sidebar's "+", "내 작업").
 */
export function readDatabaseTemplatesAhead(session: Session, dataSourceId: string,
  templates: readonly DatabaseToolbarTemplateMetadataWire[]) {
  const live = templates.filter(({ deleted }) => !deleted).sort((left, right) => Number(right.isDefault) - Number(left.isDefault));
  const named = live.find(({ isDefault }) => isDefault);
  if (named) defaults.set(dataSourceId, named.id);
  else defaults.delete(dataSourceId);
  for (const template of live.slice(0, AHEAD_PER_DATABASE)) readRowTemplateAhead(session, template.id, template);
}

/** Notes a data source's default template as the server named it, and reads it ahead for the next row. */
export function noteDefaultRowTemplate(session: Session, dataSourceId: string, template: DatabaseToolbarTemplateMetadataWire | undefined) {
  if (!template) {
    defaults.delete(dataSourceId);
    return;
  }
  defaults.set(dataSourceId, template.id);
  readRowTemplateAhead(session, template.id, template);
}

/** The default template a data source's rows made here start from, as this device last heard. */
export function heldDefaultRowTemplate(dataSourceId: string) {
  return defaults.get(dataSourceId);
}

/**
 * Lets go of a template's body read ahead while it is being changed here, a
 * read under way too, and reads it no more meanwhile — the toolbar's reads
 * as its settings change, mid-change, gave rows made just after it the body
 * without its last words (2026-11-16): rows made meanwhile take the server's
 * copy. It is read again once it is left (readRowTemplateAfterChange).
 */
export function dropRowTemplateAhead(session: Pick<Session, "user">, templateId: string) {
  const key = heldKey(session, templateId);
  held.set(key, { reading: false, at: 0, body: null, template: held.get(key)?.template, changing: true });
}

/** Reads a template left after a change here (dropRowTemplateAhead) ahead again. */
export function readRowTemplateAfterChange(session: Session, templateId: string) {
  const kept = held.get(heldKey(session, templateId));
  if (kept) kept.changing = false;
  readRowTemplateAhead(session, templateId);
}

// How long after a template was left it is read again ahead, its last words saved by then.
const TEMPLATE_READ_AFTER_EDIT_MS = 2_000;
// How many places have each template open to be changed here.
const openHere = new Map<string, number>();

/**
 * A template open to be changed here — in its window over its database or
 * whole at its own address (DatabaseTemplateEditor) — is let go ahead while
 * it is open anywhere, and read again a moment after the last place left it,
 * so its window left for its own address doesn't read it back meanwhile.
 */
export function useRowTemplateChangedHere(session: Session | null, templateId: string) {
  useEffect(() => {
    if (!session) return undefined;
    const key = heldKey(session, templateId);
    openHere.set(key, (openHere.get(key) ?? 0) + 1);
    dropRowTemplateAhead(session, templateId);
    return () => {
      const left = (openHere.get(key) ?? 1) - 1;
      if (left > 0) openHere.set(key, left);
      else openHere.delete(key);
      window.setTimeout(() => { if (!openHere.has(key)) readRowTemplateAfterChange(session, templateId); }, TEMPLATE_READ_AFTER_EDIT_MS);
    };
  }, [session, templateId]);
}

/** Who uses a template, when and where: what its template mentions become (database_template_mentions.go). */
type TemplateUse = { userId: string; name: string; timeZone: string; at: Date };

function templateUse(session: Session): TemplateUse {
  const name = session.user.name.trim() || session.user.email.split("@")[0] || "";
  return { userId: session.user.id, name, timeZone: chosenTimeZone(), at: new Date() };
}

function localMoment(use: TemplateUse) {
  let parts: Record<string, string>;
  try {
    parts = Object.fromEntries(new Intl.DateTimeFormat("en-CA", {
      timeZone: use.timeZone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23",
    }).formatToParts(use.at).map(({ type, value }) => [type, value]));
  } catch {
    return null;
  }
  return { date: `${parts.year}-${parts.month}-${parts.day}`, time: `${parts.hour}:${parts.minute}` };
}

/** A body's template mentions given their value for this use, as the server's (databaseTemplateUse.resolve). */
function resolvedSpans(spans: TextSpan[], use: TemplateUse): TextSpan[] | null {
  if (!spans.some(({ mention }) => mention === "template")) return spans;
  const moment = localMoment(use);
  if (!moment) return null;
  return spans.map((span) => {
    if (span.mention !== "template") return span;
    const { template, ...rest } = span;
    if (template === "today") return { ...rest, mention: "date", date: moment.date, text: `@${moment.date}` };
    if (template === "now") {
      return { ...rest, mention: "date", date: `${moment.date}T${moment.time}`, dateTimeZone: use.timeZone, text: `@${moment.date} ${moment.time}` };
    }
    if (template === "me") return { ...rest, mention: "person", userId: use.userId, text: `@${use.name}` };
    const { mention: _gone, ...plain } = rest;
    return plain;
  });
}

/** A template property value given its value for this use, as the server's (databaseTemplateUse.resolveValue). */
function resolvedValue(type: string, value: unknown, use: TemplateUse): unknown {
  if (type === "date" && (value === "@today" || value === "@now")) {
    const moment = localMoment(use);
    if (!moment) return undefined;
    return value === "@today" ? moment.date : { start: `${moment.date}T${moment.time}`, timeZone: use.timeZone };
  }
  if (type === "person" && Array.isArray(value) && value.includes("@me")) {
    return [...new Set(value.map((person) => person === "@me" ? use.userId : person))];
  }
  return value;
}

// The words a text block kept in its content once, which its copy's words take.
const STORED_TEXT_KEYS = ["text", "plainText", "rich", "notionRichTextSpans"];
const TEXT_BLOCK_TYPES = new Set<string>(graphTextBlockTypes);

/** A row's copy of a template's body: its blocks under new IDs, parents first, and their words as one update. */
export type RowTemplateCopy = { templateId: string; blocks: GraphBlock[]; words: Uint8Array };

/**
 * A copy of a template's body for a new row, from its read ahead here; none
 * when it isn't held, too old, or holds what this device doesn't copy.
 */
export function rowTemplateCopy(session: Session, templateId: string): RowTemplateCopy | null {
  const body = held.get(heldKey(session, templateId))?.body;
  if (!body || Date.now() - body.at > KEPT_MS) return null;
  const parents = new Map(body.blocks.map((block) => [block.id, block.parentId ?? ""]));
  const originals = new Set(body.blocks.filter((block) => block.type === "synced_block"
    && !(typeof block.content.syncedBlockId === "string" && block.content.syncedBlockId.trim())).map(({ id }) => id));
  // Up through every ancestor, however deep; each one once.
  const insideOriginal = (id: string) => {
    const seen = new Set<string>();
    for (let parent = parents.get(id) ?? ""; parent && !seen.has(parent); parent = parents.get(parent) ?? "") {
      seen.add(parent);
      if (originals.has(parent)) return true;
    }
    return false;
  };
  const children = new Map<string, GraphBlock[]>();
  for (const block of body.blocks) {
    const parent = block.parentId ?? "";
    children.set(parent, [...children.get(parent) ?? [], block]);
  }
  const order = (left: GraphBlock, right: GraphBlock) => left.position < right.position ? -1 : left.position > right.position ? 1
    : left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
  const fresh = new Map<string, string>();
  const copied = (id: string) => fresh.get(id) ?? fresh.set(id, newId()).get(id)!;
  const use = templateUse(session);
  const document = new Y.Doc();
  const blocks: GraphBlock[] = [];
  const visited = new Set<string>();
  let readable = true;
  const visit = (parentId: string) => {
    if (visited.has(parentId)) return;
    visited.add(parentId);
    let previous: string | null = null;
    for (const block of [...children.get(parentId) ?? []].sort(order)) {
      if (insideOriginal(block.id)) continue;
      const id = copied(block.id);
      let { type, content } = block;
      if (originals.has(block.id)) content = { syncedBlockId: block.id, syncedPageId: body.templateId };
      else if (type === "child_page" && content.childPageId === block.id) type = "link_to_page";
      const own = body.words.getText(BLOCK_TEXT_ROOT_PREFIX + block.id);
      const text = TEXT_BLOCK_TYPES.has(type);
      let spans = own.length ? yTextToRichText(own) : text && Array.isArray(content.rich) ? content.rich as TextSpan[] : [];
      if (text && STORED_TEXT_KEYS.some((name) => name in content)) {
        content = { ...content };
        for (const name of STORED_TEXT_KEYS) delete content[name];
      }
      spans = spans && resolvedSpans(spans, use) || undefined;
      if (!spans) readable = false;
      else if (spans.length && type !== "divider") writeRichTextToYText(document.getText(BLOCK_TEXT_ROOT_PREFIX + id), spans);
      blocks.push({ id, parentId: parentId ? copied(parentId) : null, afterId: previous, position: block.position, type, content });
      previous = id;
      visit(block.id);
    }
  };
  visit("");
  if (!readable || !blocks.length) return null;
  const words = Y.encodeStateAsUpdate(document);
  document.destroy();
  return { templateId, blocks, words };
}

/** A row about to be made here from a template: what its making sends (`template`), and its body copied here (`copy`). */
export type RowTemplateMaking = { template: RowMadeFromTemplate; copy: RowTemplateCopy | null };

/**
 * What a row about to be made here from a template sends with its making
 * (rowMadeHere.ts makeRowHere), which the server fills it from as it makes
 * it, in the same request — so its fill goes whenever the row does, after a
 * reload, a closed tab or offline too (2026-11-25: filled by a request of its
 * own once the row was saved, a row sent later stayed empty): the template,
 * and its body copied here, blocks and words, when this device holds it
 * (rowTemplateCopy) — the server copies the template's body otherwise. With
 * no `templateId`, its database's default as the server names it then; a
 * database's default (`isDefault`) is named only with its body copied here.
 * A view's default (`orDefault`) is named as one its database's default
 * stands for once it is gone, as the server then takes it.
 */
export function rowTemplateToMakeFrom(session: Session, templateId: string | undefined,
  { isDefault = false, orDefault = false }: { isDefault?: boolean; orDefault?: boolean } = {}): RowTemplateMaking {
  const copy = templateId ? rowTemplateCopy(session, templateId) : null;
  const named = templateId && (copy || !isDefault) ? { templateId, ...orDefault ? { orDefault: true } : {} } : {};
  return { template: { ...named, timeZone: chosenTimeZone(), ...copy ? { blocks: copy.blocks, words: bytesToBase64(copy.words) } : {} }, copy };
}

/**
 * A row just made here from a template (rowTemplateToMakeFrom): its body
 * copied here shows in its page's copy here (`shown`, once kept, or a moment
 * at most); `filled` settles once the server made it, filled from the
 * template (true), or refused it. Its open page is told then
 * (ROW_FILLED_FROM_TEMPLATE_EVENT), and a body the server copied is followed
 * while its own databases are copied in after the rest (lib/pageDatabaseCopies.ts).
 */
export function madeFromTemplate(session: Session, rowId: string, { template, copy }: RowTemplateMaking) {
  const shown = copy ? keepRowBodyMadeHere(session, rowId, copy.blocks, copy.words) : Promise.resolve();
  // The next row from it reads it again meanwhile, as it may have changed since.
  if (copy) readRowTemplateAhead(session, copy.templateId);
  const kept = template.templateId ? held.get(heldKey(session, template.templateId)) : undefined;
  const ownDatabases = kept?.ownDatabases ?? (kept?.template?.hasContent === false ? false : undefined);
  const filled = whenRowMadeThere(rowId).then((made) => {
    if (made) window.dispatchEvent(new CustomEvent(ROW_FILLED_FROM_TEMPLATE_EVENT, { detail: rowId }));
    followPageDatabaseCopies(session, rowId, made && !copy && ownDatabases !== false ? undefined : false);
    return made;
  });
  return { shown, filled, copied: copy !== null };
}

/** A database of a body's own, not a view of another's. */
function ownDatabase(block: GraphBlock) {
  return block.type === "inline_database" && block.content.linkedDatabaseSource !== true;
}

/**
 * A row made from a template whose body the server copies (none copied here)
 * waits for it till `filled` settles, its page offering no templates
 * meanwhile (noteRowTakingTemplate). One whose body, as read ahead, holds a
 * database of its own shows its body as being filled from the press, as
 * Notion's: the server copies those after the rest (lib/pageDatabaseCopies.ts),
 * which its answer to the template's use then follows.
 */
export function fillRowFromTemplateAhead(session: Pick<Session, "user">, rowId: string, templateId: string, filled: Promise<boolean | void>) {
  noteRowTakingTemplate(rowId, filled);
  if (held.get(heldKey(session, templateId))?.ownDatabases) setPageFilling(rowId, true);
}

/** The template a data source's toolbar offers, as read ahead here with its body. */
export function heldRowTemplate(session: Pick<Session, "user">, templateId: string) {
  return held.get(heldKey(session, templateId))?.template;
}

/**
 * The values a row made from a template is made with, as the server would
 * fill them (databaseToolbarTemplateRowValues): its title, and its values
 * the reader may set (the server leaves the others out too,
 * withoutUneditableValues), template values taking their value for this use.
 * Made with them, the row shows them at once and keeps them through the
 * server's reads, as Notion's (seen 2026-11-14: "TPL" and its status as the
 * page opened, "TPLX" with an X typed). Words whose spans hold mentions are
 * left to the server, which gives those their value.
 */
export function rowTemplateValues(session: Session, template: DatabaseToolbarTemplateMetadataWire | undefined,
  properties: readonly GraphProperty[], dataSourceId: string): Record<string, unknown> {
  if (!template) return {};
  const use = templateUse(session);
  const values: Record<string, unknown> = {};
  for (const property of properties) {
    if (property.dataSourceId !== dataSourceId) continue;
    if (property.type === "title") {
      if (template.title) values[property.id] = template.title;
      continue;
    }
    if (!(property.id in template.properties) || template.propertySpans?.[property.id]?.length) continue;
    if (property.access?.restricted && property.access.level !== "edit" && property.access.level !== "edit_values") continue;
    if (UNSET_BY_TEMPLATES.has(property.type)) continue;
    const value = resolvedValue(property.type, template.properties[property.id], use);
    if (value !== undefined && isNonEmptyStoredValue(value)) values[property.id] = value;
  }
  return values;
}

// The values a template doesn't set (databaseToolbarTemplateWritableProperty).
const UNSET_BY_TEMPLATES = new Set(["created_time", "last_edited_time", "created_by", "last_edited_by", "rollup", "formula",
  "unique_id", "button", "location", "last_visited_time"]);

/** A row made from a template as it shows until the server gives it the template's icon: with it, where it has none. */
export function rowWithTemplateIcon(row: GraphRow, template: DatabaseToolbarTemplateMetadataWire | undefined): GraphRow {
  return !template || row.icon || !template.icon.trim() ? row
    : { ...row, ...storedIconPresentation(template.icon) as Pick<GraphRow, "icon" | "iconType"> };
}

/** The default template a data source's rows made here start from, as this device last heard of it. */
export function heldDefaultTemplate(session: Session, dataSourceId: string) {
  const templateId = defaults.get(dataSourceId);
  return templateId ? { templateId, template: heldRowTemplate(session, templateId) } : null;
}
