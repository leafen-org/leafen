import type {
  WorkspaceCSVMergeNewProperty,
  WorkspaceCSVMergeTargetProperty,
} from "../workspaceImportWire.generated";
import { defaultCSVNewDatabaseChoices, type CSVDateFormat, type CSVPreviewColumn } from "./csvImportModel";

/**
 * What a CSV column fills, as Notion's "CSV 열 설정" chooses it (seen
 * 2026-10-01): "" leaves it out, `property:<id>` fills an existing property
 * and `new:<type>` a new property of that type; a new date can name its
 * display format, `new:date:<format>`, from "날짜"'s formats (seen 2026-10-05).
 */
export type CSVMergeChoice = "" | `property:${string}` | `new:${CSVMergeNewType}` | `new:date:${CSVDateFormat}`;

/** A new property's type and, for a date, the format it shows. */
export function csvMergeNewType(choice: CSVMergeChoice): { type: CSVMergeNewType; dateFormat?: CSVDateFormat } | null {
  if (!choice.startsWith("new:")) return null;
  const rest = choice.slice("new:".length);
  if (rest.startsWith("date:")) return { type: "date", dateFormat: rest.slice("date:".length) as CSVDateFormat };
  return { type: rest as CSVMergeNewType };
}

/** The types a column's new property takes, as Notion's "Add new property" lists them (seen 2026-10-01). */
export const CSV_MERGE_NEW_TYPES = [
  "rich_text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone", "person",
] as const;

export type CSVMergeNewType = (typeof CSV_MERGE_NEW_TYPES)[number];

/**
 * What each column fills at first, as Notion matches it: the property named
 * as its header; else, while no header names the title property, the first
 * column read as text fills the title; else a new property of the type its
 * rows read as, by the rule a new database's columns take theirs
 * (defaultCSVNewDatabaseChoices) — Notion's, seen 2026-11-16 in two tabs
 * merging into a database whose title is "이름": "Name,Count,Note" and
 * "Item,Score,Tagline" put Name and Item in the title and Count and Score in
 * new numbers, "Qty,Label,Extra" a new number, then Label in the title.
 */
export function defaultCSVMergeChoices(
  headers: readonly string[],
  properties: readonly WorkspaceCSVMergeTargetProperty[],
  records: readonly string[][] = [],
): CSVMergeChoice[] {
  const used = new Set<string>();
  const named = headers.map((header) => {
    const name = header.trim().toLocaleLowerCase();
    const property = properties.find((candidate) => !used.has(candidate.id)
      && candidate.name.trim().toLocaleLowerCase() === name);
    if (property) used.add(property.id);
    return property;
  });
  const read = defaultCSVNewDatabaseChoices(records, headers.length);
  const title = properties.find((property) => property.type === "title" && !used.has(property.id));
  const asText = (index: number) => !read[index] || read[index] === "title" || read[index] === "rich_text";
  const titled = title ? headers.findIndex((_, index) => !named[index] && asText(index)) : -1;
  return headers.map((_, index): CSVMergeChoice => {
    const property = named[index] ?? (index === titled ? title : undefined);
    if (property) return `property:${property.id}`;
    const type = read[index];
    return type && !asText(index) && (CSV_MERGE_NEW_TYPES as readonly string[]).includes(type)
      ? `new:${type as CSVMergeNewType}` : "new:rich_text";
  });
}

/** The properties the choices fill, column by column, and the new ones among them. */
export function csvMergeColumns(
  headers: readonly string[],
  choices: readonly CSVMergeChoice[],
  {
    newId,
    columnName,
    configFor,
  }: {
    newId: () => string;
    /** The name of a column whose header is blank. */
    columnName: (index: number) => string;
    configFor: (type: CSVMergeNewType) => Record<string, unknown>;
  },
): { columnPropertyIds: string[]; newProperties: WorkspaceCSVMergeNewProperty[] } {
  const newProperties: WorkspaceCSVMergeNewProperty[] = [];
  const columnPropertyIds = headers.map((header, index) => {
    const choice = choices[index] ?? "";
    if (choice.startsWith("property:")) return choice.slice("property:".length);
    const made = csvMergeNewType(choice);
    if (!made) return "";
    const id = newId();
    newProperties.push({
      id, name: header.trim() || columnName(index), type: made.type,
      config: { ...configFor(made.type), ...(made.dateFormat ? { dateFormat: made.dateFormat } : {}) },
    });
    return id;
  });
  return { columnPropertyIds, newProperties };
}

/**
 * The columns rows added to a database show, as Notion's preview has them
 * (seen 2026-10-01): the CSV's columns in its order, each as the property it
 * fills, then the database's other properties in its own order.
 */
export function csvMergePreviewColumns(
  headers: readonly string[],
  choices: readonly CSVMergeChoice[],
  properties: readonly WorkspaceCSVMergeTargetProperty[],
): CSVPreviewColumn[] {
  const filled = choices.flatMap((choice, index): CSVPreviewColumn[] => {
    const made = csvMergeNewType(choice);
    if (made) return [{ key: `new-${index}`, name: headers[index] ?? "", type: made.type, source: index }];
    const property = properties.find((candidate) => choice === `property:${candidate.id}`);
    return property ? [{
      key: property.id, name: property.name, type: property.type, title: property.type === "title", source: index,
    }] : [];
  });
  const others = properties.filter((property) => !choices.includes(`property:${property.id}`)).map((property) => ({
    key: property.id, name: property.name, type: property.type, title: property.type === "title", source: -1,
  }));
  return [...filled, ...others];
}

/** A database's properties as "기존 속성" lists them: the title property last (seen 2026-10-01). */
export function csvMergeMenuProperties<T extends { type: string }>(properties: readonly T[]): T[] {
  return [
    ...properties.filter((property) => property.type !== "title"),
    ...properties.filter((property) => property.type === "title"),
  ];
}

/**
 * The choices with a column filling an existing property, as Notion's menu
 * takes one (seen 2026-11-13): no property is greyed out for being taken,
 * and a column that had it fills a new property of its type instead — the
 * title's a new text property, as is one of a type no new property takes.
 */
export function chooseCSVMergeProperty(
  choices: readonly CSVMergeChoice[],
  column: number,
  value: `property:${string}`,
  properties: readonly WorkspaceCSVMergeTargetProperty[],
): CSVMergeChoice[] {
  const type = properties.find((property) => `property:${property.id}` === value)?.type ?? "";
  const moved: CSVMergeChoice = (CSV_MERGE_NEW_TYPES as readonly string[]).includes(type)
    ? `new:${type as CSVMergeNewType}` : "new:rich_text";
  return choices.map((choice, index) => index === column ? value : choice === value ? moved : choice);
}
