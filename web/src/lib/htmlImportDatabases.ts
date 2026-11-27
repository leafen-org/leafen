import { collapse } from "./importMarkdownBlocks";

/** An export's line linking a database its page holds inline (pageLinkBlock). */
export const INLINE_DATABASE_LINK = "data-leafen-inline-database";

/**
 * Notion's HTML export draws an inline database in its page as the
 * database's table of rows ("collection-content", its ID the database's),
 * where its Markdown export links the database's CSV, which the HTML export
 * has too (seen 2026-11-13); Leafen's HTML export draws one the same way.
 * Brought back, the database is that CSV's, standing in its place inline,
 * with its rows, properties and views: the table becomes a line linking the
 * CSV's database marked inline, where the importer stands the database's
 * block and then turns it inline (workspace_import_inline_databases.go).
 * One whose CSV the import lacks keeps its table.
 */
export function withNotionDatabaseLinks(html: string, databaseHref: (id: string) => string | undefined) {
  if (!html.includes("collection-content")) return html;
  const document = new DOMParser().parseFromString(html, "text/html");
  let linked = false;
  for (const database of Array.from(document.querySelectorAll("div.collection-content[id]"))) {
    const href = databaseHref(database.id);
    if (!href) continue;
    const line = document.createElement("figure");
    line.className = "link-to-page";
    line.setAttribute(INLINE_DATABASE_LINK, "");
    const link = line.appendChild(document.createElement("a"));
    link.setAttribute("href", href);
    link.textContent = collapse(database.querySelector(".collection-title")?.textContent ?? "");
    database.replaceWith(line);
    linked = true;
  }
  return linked ? document.documentElement.outerHTML : html;
}
