import { readDatabaseRowPage } from "@/databaseRowPageApi";
import { loadSession } from "@/lib/authSessionStore";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { rowMadeNamingTemplate } from "@/lib/rowMadeHere";
import { heldDefaultRowTemplate } from "@/lib/rowTemplateHere";
import { cellFieldBlank } from "./tableCellKeys";
import { cellLine, cellOnLine } from "./tableCellLine";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

// A page a database's new-page shortcut makes, or a table's "+ 새 페이지"
// opened as its page, left as it was made, leaves nothing, as Notion's (seen
// 2026-10-30: the row was gone when the database was read again); its
// "새로 만들기" and a board's card stay. A row stays fresh until this person
// writes, pastes or drops in it or presses one of its values; once they leave
// it fresh — nothing on the screen names it any more — it goes if the server
// still has no title, body, icon or cover for it and its values are those it
// was made with. What anyone wrote keeps it: a value given it by another
// person, an agent or an automation as much as by this person. A table's row
// left in its title goes only with Escape (watchFreshTableRow). A row made
// from a template stays however it is left (filledFromTemplate).

/** The values a row was made with: its empty title, and its group's or filter's. */
export type FreshRowValues = Readonly<Record<string, { value: unknown } | undefined>>;

const fresh = new Map<string, { containerPageId: string; made: FreshRowValues }>();
const LEAVE_SETTLE_MS = 400;

// The values people set, not those Leafen works out.
const VALUES_SET_BY_PEOPLE = new Set(["title", "rich_text", "number", "url", "email", "phone", "select", "multi_select", "status", "person",
  "relation", "date", "files", "checkbox"]);

/** Marks a row just made, with the page holding its database and the values it was made with. */
export function markFreshRow(rowId: string, containerPageId: string, made: FreshRowValues = {}) {
  fresh.set(rowId, { containerPageId, made });
}

export function isFreshRow(rowId: string) {
  return fresh.has(rowId);
}

/**
 * Whether a template this device knows of fills a row made here, as its
 * making named it (rowMadeHere.ts): one by id, or its database's default as
 * the server names it, which this device has heard it has
 * (heldDefaultRowTemplate). Such a row stays, empty title and all, as
 * Notion keeps a row its default template filled (seen 2026-11-25 in two
 * tabs: Escape in its title, still empty, chose the title and kept the row
 * with the template's body, the next letter typed into its title; a press
 * outside kept it). Judged by its title on screen alone, a row whose
 * template has no title went (2026-11-25). One whose default isn't known
 * here yet — its toolbar not having read the templates, as the page opens or
 * offline — goes as an empty row does: Notion's first "+ 새 페이지" after a
 * load, its template not yet with it, went with Escape at once (seen twice,
 * the next press kept), as with the empty page as the default.
 */
export function filledFromTemplate(rowId: string) {
  const named = rowMadeNamingTemplate(rowId);
  return named !== undefined && ("templateId" in named || heldDefaultRowTemplate(named.dataSourceId) !== undefined);
}

/** The person did something to the row: it stays. */
function touchFreshRow(rowId: string) {
  fresh.delete(rowId);
}

/**
 * Follows edits under root to a fresh row: writing, pasting or dropping where
 * `within` says, or pressing where `pressKeeps` says (its values, icon or
 * cover), touches it. Answers the listeners' removal.
 */
export function followFreshRowEdits(
  root: HTMLElement, rowId: string, pressKeeps: (target: Element) => boolean, within: (target: Element) => boolean = () => true,
) {
  const inside = (event: Event) => event.target instanceof Element && within(event.target);
  const edit = (event: Event) => { if (inside(event)) touch(); };
  const press = (event: Event) => { if (inside(event) && pressKeeps(event.target as Element)) touch(); };
  const touch = () => { touchFreshRow(rowId); stop(); };
  const stop = () => {
    for (const type of ["beforeinput", "paste", "drop"]) root.removeEventListener(type, edit, true);
    root.removeEventListener("pointerdown", press, true);
  };
  for (const type of ["beforeinput", "paste", "drop"]) root.addEventListener(type, edit, true);
  root.addEventListener("pointerdown", press, true);
  return stop;
}

/**
 * Follows a fresh table row made with its title to type in. Escape in that
 * title showing no words takes the row away at once and chooses the cell
 * above it (`discard`), unless a template fills the row (filledFromTemplate); its
 * title left any other way — a press elsewhere,
 * Tab, Enter — keeps the row, empty or not, as Notion's (seen 2026-11-14:
 * Escape took away an empty row and one typed and erased again, choosing the
 * title above; a press outside, Tab or Enter kept it, a reload too). A row
 * opened as its page meanwhile is that page's to follow (DatabaseRowPage).
 */
export function watchFreshTableRow(rowId: string, discard: (rowId: string) => void) {
  const title = () => document.querySelector<HTMLElement>(`[data-table-row-id="${CSS.escape(rowId)}"] > [data-table-cell][data-title="true"]`);
  const inTitle = (target: EventTarget | null) => {
    const cell = title();
    return Boolean(cell && target instanceof Node && cell !== target && cell.contains(target));
  };
  const escape = (event: KeyboardEvent) => {
    if (!fieldEscape(event) || event.shiftKey || event.altKey || event.metaKey || event.ctrlKey) return;
    if (!fresh.has(rowId)) { stop(); return; }
    const cell = title();
    if (!cell || !inTitle(event.target) || !cellFieldBlank(event.target)) return;
    // Its template's: Escape only leaves its title, as in any row's.
    if (filledFromTemplate(rowId)) { stop(); fresh.delete(rowId); return; }
    event.preventDefault();
    event.stopImmediatePropagation();
    stop();
    fresh.delete(rowId);
    const line = cellLine(cell);
    const above = line > 0 ? cellOnLine(cell.closest('[role="grid"]'), line - 1, cell.dataset.colIndex) : null;
    discard(rowId);
    window.requestAnimationFrame(() => { if (above?.isConnected) above.focus({ preventScroll: true }); });
  };
  // Left: kept, unless its page opened meanwhile.
  const away = (event: Event) => {
    if (!fresh.has(rowId)) { stop(); return; }
    const left = event instanceof FocusEvent ? inTitle(event.target) && !inTitle(event.relatedTarget) : !inTitle(event.target);
    if (!left) return;
    stop();
    window.setTimeout(() => {
      if (window.location.href.includes(rowId) || document.querySelector(`[data-standalone-row="${CSS.escape(rowId)}"]`)) return;
      fresh.delete(rowId);
    }, LEAVE_SETTLE_MS);
  };
  const stop = () => {
    document.removeEventListener("keydown", escape, true);
    document.removeEventListener("pointerdown", away, true);
    document.removeEventListener("focusout", away, true);
  };
  document.addEventListener("keydown", escape, true);
  document.addEventListener("pointerdown", away, true);
  document.addEventListener("focusout", away, true);
}

/**
 * Lets a fresh row go once the screen settles without it — no address and
 * no open row page naming it, the focus not in its table row.
 */
export function leaveFreshRowSoon(rowId: string) {
  if (!fresh.has(rowId)) return;
  window.setTimeout(() => {
    if (!fresh.has(rowId) || window.location.href.includes(rowId)) return;
    const named = document.querySelector(`[data-table-row-id="${CSS.escape(rowId)}"]:focus-within, [data-standalone-row="${CSS.escape(rowId)}"]`);
    if (named) return;
    void leaveFreshRow(rowId);
  }, LEAVE_SETTLE_MS);
}

// Nothing: no value, or an empty, cleared or unchecked one.
const held = (value: unknown) => !(value === undefined || value === null || value === "" || value === false
  || (Array.isArray(value) && value.length === 0));

// Two values alike, a field left out of one as one held empty in the other.
function sameValue(left: unknown, right: unknown): boolean {
  if (!held(left) || !held(right)) return !held(left) && !held(right);
  if (typeof left !== "object" || typeof right !== "object") return left === right;
  if (Array.isArray(left) !== Array.isArray(right)) return false;
  const a = left as Record<string, unknown>, b = right as Record<string, unknown>;
  return [...new Set([...Object.keys(a), ...Object.keys(b)])].every((key) => sameValue(a[key], b[key]));
}

async function leaveFreshRow(rowId: string) {
  const left = fresh.get(rowId), session = loadSession();
  fresh.delete(rowId);
  if (!session || !left || filledFromTemplate(rowId)) return;
  const snapshot = await readDatabaseRowPage(session, rowId, { limit: 1 }).catch(() => null);
  if (!snapshot || snapshot.body.length || snapshot.row.icon || snapshot.row.cover || snapshot.row.trashedAt) return;
  const title = snapshot.properties.find((property) => property.type === "title");
  const value = title ? snapshot.row.values[title.id]?.value : "";
  if (typeof value === "string" ? value.trim() : value) return;
  if (snapshot.properties.some((property) => VALUES_SET_BY_PEOPLE.has(property.type) && property.type !== "title"
    && !sameValue(snapshot.row.values[property.id]?.value, left.made[property.id]?.value))) return;
  await sendContentGraphChange(session, left.containerPageId, [{ kind: "deleteRow", rowDeletion: { id: rowId } }]).catch(() => undefined);
}
