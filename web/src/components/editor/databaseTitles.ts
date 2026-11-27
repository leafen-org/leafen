import { i18next } from "@/i18n";

export const DEFAULT_DATABASE_TITLE = "";

// Locale-aware display default for a new inline database. It is shown only as
// a placeholder and never stored as the database's title.
export function inlineDatabasePlaceholderTitle(): string {
  return i18next.t("databaseTitles:inlineDatabasePlaceholder");
}

/**
 * What an untitled data source is called, as Notion's (seen 2026-10-30):
 * while it is its database's only source it is the database, "새
 * 데이터베이스"; once the database holds more than one, each untitled one is
 * "새 데이터 소스". A source made in a database is made with no name.
 */
export function untitledDataSourceTitle(sourceCount = 1): string {
  return sourceCount > 1 ? i18next.t("databaseView:newDataSource") : inlineDatabasePlaceholderTitle();
}

export function dataSourceDisplayTitle(title: string | undefined, sourceCount = 1): string {
  return title?.trim() || untitledDataSourceTitle(sourceCount);
}

/**
 * One of the names an untitled database of several sources is listed by: a
 * source's title, or — one the database shows from another — that title
 * marked linked, as the server sends it (internal/app/database_source_titles.go).
 */
export type DatabaseSourceTitle = string | { title: string; linked: true };

/** Whether a value read from a stored or projected block is one of those names. */
export function isDatabaseSourceTitle(value: unknown): value is DatabaseSourceTitle {
  if (typeof value === "string") return true;
  const entry = value as { title?: unknown; linked?: unknown } | null;
  return typeof entry === "object" && entry !== null && typeof entry.title === "string" && entry.linked === true;
}

/** The title one of those names holds. */
export function databaseSourceTitleText(entry: DatabaseSourceTitle): string {
  return typeof entry === "string" ? entry : entry.title;
}

/** Whether one of those names is a source the database shows from another. */
export function databaseSourceTitleLinked(entry: DatabaseSourceTitle): boolean {
  return typeof entry !== "string" && entry.linked === true;
}

/**
 * An untitled database's name while it holds several sources, as Notion's
 * page title, path and window name it (seen 2026-11-02): two are "A 및 B",
 * more "A, B, C" — a source it shows from another database by that one's
 * linked view's name, "C 및 X 보기", "A, B, X 보기" (seen 2026-11-25 in its
 * placeholder opened alone, window, path and mentions; linkedDatabaseName).
 */
export function untitledDatabaseSourcesName(titles: readonly DatabaseSourceTitle[]): string {
  const names = titles.map((entry) => databaseSourceTitleLinked(entry)
    ? linkedDatabaseName(undefined, databaseSourceTitleText(entry))
    : dataSourceDisplayTitle(databaseSourceTitleText(entry), titles.length));
  return names.length === 2 ? names.join(i18next.t("databaseTitles:sourcesAnd")) : names.join(", ");
}

/**
 * A linked database's name: its own, or "〈source〉 보기" while it has none,
 * as Notion's names it in its window, its page and its rename field (seen
 * 2026-10-15).
 */
export function linkedDatabaseName(ownTitle: string | undefined, sourceTitle: string): string {
  return ownTitle?.trim() || i18next.t("databaseView:savedViewTitle", { title: sourceTitle.trim() || inlineDatabasePlaceholderTitle() });
}

const DATABASE_SLASH_TITLE_ALIASES = new Set([
  "board",
  "/board",
  "calendar",
  "/calendar",
  "database",
  "/database",
  "db",
  "/db",
  "gallery",
  "/gallery",
  "inline database",
  "/inline database",
  "/inline-database",
  "/inline_database",
  "inline-database",
  "inline_database",
  "list",
  "/list",
  "table",
  "/table",
  "timeline",
  "/timeline",
]);

function normalizeDatabaseTitle(title: string | undefined) {
  return title
    ?.normalize("NFKC")
    .replace(/[\u200B-\u200D\uFEFF]/g, "")
    .replace(/\u00A0/g, " ")
    .trim()
    .toLowerCase()
    .replace(/\s+/g, " ");
}

function isDatabaseSlashCommandTitle(title: string | undefined) {
  const normalized = normalizeDatabaseTitle(title);
  return !!normalized && DATABASE_SLASH_TITLE_ALIASES.has(normalized);
}

function meaningfulInlineDatabaseTitle(title: string | undefined) {
  const clean = title?.trim();
  return clean
    && !isDatabaseSlashCommandTitle(clean)
    ? clean
    : undefined;
}

/**
 * Resolve the inline-database title to render plus whether it is only the
 * placeholder. `ownTitle` must be the raw stored title, never a display
 * fallback, so clearing a title restores the placeholder without persisting it.
 */
export function inlineDatabaseTitleDisplay(input: {
  ownTitle?: string;
  importedSurfaceTitle?: string;
  resolvedLinkedTitle?: string;
  preferResolvedLinked?: boolean;
  /** What stands in for no name (untitledDataSourceTitle); a database's by default. */
  placeholder?: string;
}): { text: string; isPlaceholder: boolean } {
  const imported = meaningfulInlineDatabaseTitle(input.importedSurfaceTitle);
  const own = meaningfulInlineDatabaseTitle(input.ownTitle) ? input.ownTitle : undefined;
  const resolvedLinked = meaningfulInlineDatabaseTitle(input.resolvedLinkedTitle);
  const text =
    (input.preferResolvedLinked ? resolvedLinked : undefined) ||
    imported ||
    own ||
    resolvedLinked ||
    input.placeholder ||
    inlineDatabasePlaceholderTitle();
  const isPlaceholder = !imported && !own && !resolvedLinked;
  return { text, isPlaceholder };
}
