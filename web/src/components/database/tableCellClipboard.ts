import type { GraphProperty, GraphRow, GraphRowValue } from "@/contentGraph";
import { readCSVCheckbox, readCSVDate, readCSVNumber } from "@/lib/csvImportModel";
import { directoryPeople, personName } from "@/lib/peopleDirectory";
import type { DbProperty, Page } from "@/lib/types";
import { absolutePageUrl } from "@/lib/navigation";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { evaluateFormula, evaluateRollupShownValues, evaluateRollupWithComputedDisplay, formatFormulaValue, formulaDisplay } from "./formula";
import type { FormulaBounds } from "./formulaBudget";
import { formulaDateText } from "./formulaDateText";
import { formulaTimeZone } from "./formulaDates";
import { formulaLocale } from "./formulaLocales";
import { FormulaDate, type FormulaValue } from "./formulaSyntax";
import { flatFormulaItems, formulaReferenceText, isFormulaReference } from "./formulaValues";
import { lineWithClosedMentions } from "@/lib/closedMentions";
import { rollupShownText } from "./rollupDateDisplay";
import { rollupShownFunction } from "./rollupPolicy";
import { rollupFormatProperty, rollupNumberText } from "./rollupNumberDisplay";
import { valueAsRead } from "./valueAsRead";
import { copiedDateForProperty, dateKey, formatDateForProperty, instantOnViewerClock, parseDateDraftIn, withDateRange } from "./dateUtils";
import { fileAttachmentAddress, normalizeFileAttachments } from "./files";

// Copied cells read as Notion's copy gives them (seen 2026-11-02, 11-04,
// 11-06, 11-11): a number "3.5", an option's name, options joined by ","
// ("MA,MB"), people's names joined by ", ", related pages' addresses joined
// by ", ", files' addresses joined by ",", "Yes" or "No", a formula's or a
// rollup's values joined by "," (formulaCopyText); a paste reads that
// back for its column's type, and what its type can't read leaves the cell
// empty (Notion's, seen 2026-11-02 for a date).

export type TableClipboardPages = {
  /** The pages a row's relation may name, by their database. */
  pagesById: Readonly<Record<string, Page>>;
  /** What a formula or a rollup is worked out with, as its cell works it out: each database's properties and the bounds. */
  propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  bounds?: FormulaBounds;
};

/**
 * A formula's value as a copy of its cell gives it, as Notion's (seen 2026-11-12):
 * a list's items flat, empty lists left out, between commas with no space
 * ("true,false", "a, b,c"); a date, a person or a page as its mention, "@"
 * first, alone too ("true,@대상 켬", "@2026년 1월 2일"), a date written as
 * format() writes it, on its own clock ("@2026년 1월 2일 오전 10:30 (UTC)");
 * anything else as its cell shows it.
 */
function formulaCopyText(value: FormulaValue, bounds: FormulaBounds, untitled: string) {
  const viewer = { timeZone: formulaTimeZone(undefined), locale: formulaLocale() };
  const item = (part: FormulaValue) => (part instanceof FormulaDate ? `@${formulaDateText(part, viewer)}`
    : isFormulaReference(part) ? `@${formulaReferenceText(part, untitled)}` : formatFormulaValue(part, bounds, untitled));
  return lineWithClosedMentions(flatFormulaItems(value).map(item).join(","));
}

/** A related value a rollup shows as it is, as a copy of its own cell gives it; a title without a property. */
function rolledUpText(target: DbProperty | undefined, value: unknown, pages: TableClipboardPages, bounds: FormulaBounds): string {
  switch (target?.type ?? "title") {
    case "title":
      return typeof value === "string" ? value : "";
    case "formula":
      return formulaCopyText(value as FormulaValue, bounds, untitledPageDisplayTitle("page"));
    case "checkbox":
      return value === true ? "Yes" : "No";
    case "created_time":
    case "last_edited_time":
      return value ? formatDateForProperty(instantOnViewerClock(value), target as never) : "";
    case "created_by":
    case "last_edited_by":
      return typeof value === "string" && value ? personName(value) : "";
  }
  return storedCellText(target as unknown as CopiedProperty, value, pages);
}

/** A formula's or a rollup's text as a copy of its cell gives it, worked out here. */
function computedCellText(page: Page, prop: DbProperty, pages: TableClipboardPages, bounds: FormulaBounds): string {
  const propsByDb = (pages.propsByDatabaseId ?? {}) as Record<string, DbProperty[]>;
  const pagesById = pages.pagesById as Record<string, Page>;
  const props = propsByDb[prop.databaseId] ?? [];
  if (prop.type === "formula") {
    const untitled = untitledPageDisplayTitle("page");
    const value = evaluateFormula({ row: page, prop, props, pagesById, propsByDb, bounds });
    const display = formulaDisplay(page, prop, bounds, untitled, () => value);
    // A number in the formula's own format copies as it shows.
    return display.number === undefined ? formulaCopyText(value, bounds, untitled) : display.text;
  }
  const relation = props.find((property) => property.id === prop.config?.rollupRelationPropertyId);
  const targetProps = propsByDb[relation?.config?.relationDatabaseId ?? prop.databaseId] ?? [];
  const rollupInput = { row: page, prop, sourceProps: props, targetProps, pagesById, propsByDb, bounds };
  // Related values shown as they are copy each as a copy of its own cell
  // gives it, between commas with no space, as Notion's (seen 2026-11-12:
  // "Yes,No", "대상 켬,대상 끔", "2026년 1월 2일 오전 10:30 (UTC),2026년 1월 5일").
  const shown = evaluateRollupShownValues(rollupInput);
  if (shown) {
    const texts = shown.values.map((value) => rolledUpText(shown.target as DbProperty | undefined, value, pages, bounds)).filter(Boolean);
    return lineWithClosedMentions((rollupShownFunction(prop.config) === "show_unique" ? [...new Set(texts)] : texts).join(","));
  }
  const text = rollupShownText(prop.config?.rollupFunction, evaluateRollupWithComputedDisplay(rollupInput));
  const target = targetProps.find((property) => property.id === prop.config?.rollupTargetPropertyId);
  return rollupNumberText(prop.config?.rollupFunction, text, target ? rollupFormatProperty(target, targetProps, propsByDb) : undefined, bounds.rollup);
}

type Option = { id: string; name: string };
type CopiedProperty = Pick<GraphProperty, "type" | "config">;

function options(property: CopiedProperty): Option[] {
  const raw = (property.config as { options?: unknown } | undefined)?.options;
  return Array.isArray(raw)
    ? raw.flatMap((option) => typeof option?.id === "string" && typeof option?.name === "string" ? [{ id: option.id, name: option.name }] : [])
    : [];
}

function ids(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string") : typeof value === "string" && value ? [value] : [];
}

function relationTarget(property: GraphProperty) {
  return (property.config as { relationDatabaseId?: string } | undefined)?.relationDatabaseId ?? property.dataSourceId;
}

/**
 * A formula's or a rollup's text as a copy of its cell gives it, worked out
 * for the row's page with its database's own property, which names the
 * properties prop() reads, as its cell is.
 */
export function computedValueText(rowId: string, property: DbProperty, pages: TableClipboardPages): string {
  const page = pages.pagesById[rowId];
  const prop = pages.propsByDatabaseId?.[property.databaseId]?.find((candidate) => candidate.id === property.id) ?? property;
  return page && pages.bounds ? computedCellText(page, prop, pages, pages.bounds) : "";
}

/** A cell's text as a copy gives it to other apps. */
export function tableCellText(property: GraphProperty, row: GraphRow, pages: TableClipboardPages): string {
  // What is worked out reads as the row's page shows it.
  switch (property.type) {
    case "title":
      return pages.pagesById[row.id]?.title ?? String(row.values[property.id]?.value ?? "");
    case "formula":
    case "rollup":
      return computedValueText(row.id, { ...property, databaseId: property.dataSourceId } as unknown as DbProperty, pages);
    case "created_time":
      return row.createdAt ? formatDateForProperty(instantOnViewerClock(row.createdAt), property as never) : "";
    case "last_edited_time":
      return row.updatedAt ? formatDateForProperty(instantOnViewerClock(row.updatedAt), property as never) : "";
    case "created_by":
      return row.createdBy ? personName(row.createdBy) : "";
    case "last_edited_by":
      return row.lastEditedBy ? personName(row.lastEditedBy) : "";
    case "checkbox":
      // An untouched box copies as "No" too, as Notion's (seen 2026-11-06: "Yes" / "No").
      return row.values[property.id]?.value === true ? "Yes" : "No";
    case "unique_id": {
      const number = row.values[property.id]?.value;
      if (number === null || number === undefined || number === "") return "";
      const prefix = (property.config as { idPrefix?: string } | undefined)?.idPrefix?.trim();
      return prefix ? `${prefix}-${number}` : String(number);
    }
  }
  // A status that sets none copies its default, as the cell shows it (valueAsRead).
  return storedCellText(property, valueAsRead(row, property, row.values[property.id]?.value), pages);
}

/** A stored value's text as a copy of a cell of its property gives it. */
function storedCellText(property: CopiedProperty, stored: unknown, pages: TableClipboardPages): string {
  if (stored === null || stored === undefined || stored === "") return "";
  switch (property.type) {
    case "select":
    case "status":
    case "multi_select": {
      const byId = new Map(options(property).map((option) => [option.id, option.name]));
      return ids(stored).map((id) => byId.get(id) ?? "").filter(Boolean).join(",");
    }
    case "person":
      return ids(stored).map((id) => personName(id)).filter(Boolean).join(", ");
    case "relation":
      // A related page copies as its address, which a paste reads back.
      return ids(stored).filter((id) => pages.pagesById[id] && !pages.pagesById[id].inTrash)
        .map((id) => absolutePageUrl(id)).join(", ");
    case "date":
      return copiedDateForProperty(stored, property as never);
    case "files":
      // A linked file copies as its address, as written where that opens
      // nothing ("abc"); one uploaded here, which has no
      // lasting address of its own, as its stored one or else its name.
      return normalizeFileAttachments(stored).map((file) => (file.source === "external"
        ? fileAttachmentAddress(file) || file.url.trim() : /^https?:\/\//iu.test(file.url) ? file.url : file.name)).filter(Boolean).join(",");
    default:
      return typeof stored === "object" ? "" : String(stored);
  }
}

const READ_ONLY = new Set(["formula", "rollup", "unique_id", "created_time", "last_edited_time", "created_by", "last_edited_by",
  "button", "verification", "files", "place"]);

/** Whether a paste writes this column: not what is worked out or is a file. */
export function tableColumnTakesPaste(property: GraphProperty) {
  return !READ_ONLY.has(property.type);
}

function splitList(text: string) {
  return text.split(/\s*,\s*/u).map((part) => part.trim()).filter(Boolean);
}

/**
 * The options pasted text names: its comma pieces, a run of them that is one
 * option's name together taken as that option — one named with a comma
 * before option names held none (optionNameText.ts), as a copy of its cell
 * gives it.
 */
function optionsNamedIn(text: string, all: readonly Option[]) {
  const parts = text.split(",");
  const chosen: string[] = [];
  for (let start = 0; start < parts.length;) {
    let found: Option | undefined;
    let end = parts.length;
    for (; end > start && !found; end -= 1) found = byName(all, parts.slice(start, end).join(","));
    if (found) chosen.push(found.id);
    start = found ? end + 1 : start + 1;
  }
  return chosen;
}

function byName<T extends { name: string }>(list: readonly T[], name: string) {
  const wanted = name.trim().toLocaleLowerCase();
  return list.find((item) => item.name.trim().toLocaleLowerCase() === wanted);
}

/** The value pasted text gives a cell of this column, null when its type reads nothing in it. */
export function tableCellFromText(property: GraphProperty, text: string, pages: TableClipboardPages): GraphRowValue {
  const typed = text.trim();
  const value = (next: unknown): GraphRowValue => ({ type: property.type, value: next });
  if (!typed) return value(null);
  switch (property.type) {
    case "number":
      return value(readCSVNumber(typed));
    case "checkbox":
      return value(readCSVCheckbox(typed) ?? false);
    case "select":
    case "status":
      return value(byName(options(property), typed)?.id ?? null);
    case "multi_select": {
      const chosen = optionsNamedIn(typed, options(property));
      return value(chosen.length ? [...new Set(chosen)] : null);
    }
    case "person": {
      const people = directoryPeople().map((person) => ({ id: person.userId, name: person.displayName ?? "", email: person.email ?? "" }));
      const chosen = splitList(typed).map((name) => (byName(people, name)
        ?? people.find((person) => person.email && person.email.toLocaleLowerCase() === name.toLocaleLowerCase()))?.id)
        .filter((id): id is string => Boolean(id));
      return value(chosen.length ? [...new Set(chosen)] : null);
    }
    case "relation": {
      const target = relationTarget(property);
      const rows = Object.values(pages.pagesById)
        .filter((page) => page.parentType === "database" && page.parentId === target && !page.inTrash)
        .map((page) => ({ id: page.id, name: page.title ?? "" }));
      // A page's address (as a copy gives it) or its name.
      const addressed = (part: string) => {
        const id = /\/p\/([^/?#\s]+)/u.exec(part)?.[1];
        return id ? rows.find((row) => row.id === id) : undefined;
      };
      const chosen = splitList(typed).map((name) => (addressed(name) ?? byName(rows, name))?.id).filter((id): id is string => Boolean(id));
      return value(chosen.length ? [...new Set(chosen)] : null);
    }
    case "date": {
      const dateFormat = (property.config as { dateFormat?: string } | undefined)?.dateFormat;
      const day = (part: string) => {
        const parsed = parseDateDraftIn(part, dateFormat);
        return parsed ? dateKey(parsed) : readCSVDate(part) ?? "";
      };
      const [start = "", end = ""] = typed.split(/\s*→\s*/u).map(day);
      return value(start ? withDateRange(null, start, end) : null);
    }
    default:
      return value(typed);
  }
}
