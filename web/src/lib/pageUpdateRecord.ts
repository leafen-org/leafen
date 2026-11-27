import type { PageHistorySnapshotResponse } from "@/pageHistoryWire.generated";
import { addedText, type PageUpdateDiff } from "./pageUpdates";
import { richTextParts, type RichTextParts } from "./richTextSummary";
import type { TextSpan } from "./types";

// A database page's record update: what it changed of its properties and its
// title, as Notion's updates tab and workspace updates list a row's edit
// apart from its body's (seen 2026-11-13): each changed property's icon and
// name over its new value — a text's added words marked as a line's are, an
// option as its chip, a number as it reads — in the database's order, then
// the title under its property's name. A value cleared shows what it was,
// struck, as the workspace's updates show it (seen 2026-10-19).

/** A database's property, as a version's values are shown with (PageHistoryProperties). */
export type PageUpdateRecordProperty = { id: string; name: string; type: string; position?: string; config?: Record<string, unknown> };

export type PageUpdateProperty = {
  property: PageUpdateRecordProperty;
  /** Its value before the update and after it, as the page's history keeps them. */
  before: unknown;
  after: unknown;
  /** A text value's words split around what the update added to them, and its spans so (richTextSummary.ts). */
  words?: RichTextParts<string>;
  spans?: RichTextParts<TextSpan[]>;
  /** Cleared: its old value shows, struck. */
  cleared?: true;
};

type RecordState = Pick<PageHistorySnapshotResponse, "title" | "values">;

/** The kinds whose value is words, whose added part is marked. */
const textTypes = new Set(["rich_text", "url", "email", "phone_number"]);

/** A value that holds nothing: none, no words, no options. */
function empty(value: unknown) {
  return value === null || value === undefined || value === "" || (Array.isArray(value) && value.length === 0);
}

function same(left: unknown, right: unknown) {
  if (empty(left) && empty(right)) return true;
  return JSON.stringify(left ?? null) === JSON.stringify(right ?? null);
}

/** By code unit, as the server orders positions: a locale's order puts "{" before letters. */
const byUnits = (left: string, right: string) => left < right ? -1 : left > right ? 1 : 0;

/** Its properties in the database's order, as the workspace's updates list them (workspace_activity_properties.go). */
function inOrder(properties: readonly PageUpdateRecordProperty[]) {
  return [...properties].sort((left, right) => byUnits(left.position ?? "", right.position ?? "") || byUnits(left.id, right.id));
}

/**
 * What an update changed of a database page's title and properties, against
 * the page before it (none at its making); its values against those it was
 * made with where it became a database's page inside the update (`made`, its
 * owner a wiki gave it among them), as Notion's (seen 2026-11-17: a wiki's new
 * page lists its title alone). Its title given while it was still a page is a
 * page's, as Notion keeps it ("Aa 제목").
 */
export function pageUpdateRecordDiff(
  before: RecordState | null,
  after: RecordState,
  properties: readonly PageUpdateRecordProperty[],
  made?: RecordState | null,
): PageUpdateDiff {
  const changed: PageUpdateProperty[] = [];
  const valuesBefore = made ?? before;
  for (const property of inOrder(properties)) {
    if (property.type === "title") continue;
    const was = valuesBefore?.values?.[property.id]?.value ?? null;
    const now = after.values?.[property.id]?.value ?? null;
    if (same(was, now)) continue;
    if (empty(now)) {
      changed.push({ property, before: was, after: now, cleared: true });
      continue;
    }
    if (!textTypes.has(property.type) || typeof now !== "string") {
      changed.push({ property, before: was, after: now });
      continue;
    }
    const words = addedText(typeof was === "string" ? was : "", now);
    const rich = after.values?.[property.id]?.spans;
    const spans = Array.isArray(rich) ? richTextParts(rich as TextSpan[], now, words) : undefined;
    changed.push({ property, before: was, after: now, words, ...(spans ? { spans } : {}) });
  }
  const title = after.title.trim();
  const titled = (before?.title ?? "").trim() !== title && title;
  const asPage = !after.values || Object.keys(after.values).length === 0;
  return {
    ...(titled ? { title, titleParts: addedText((before?.title ?? "").trim(), title), ...(asPage ? { pageTitle: true as const } : {}) } : {}),
    lines: [],
    properties: changed,
  };
}
