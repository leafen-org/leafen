import type { ComponentProps } from "react";
import { FileBlank } from "@/icons/leafen";
import { hasExplicitPageIcon, PageIconGlyph } from "../page/PageIcon";

/** A related page's group: the icon that page shows, or true for a page's glyph. */
export type GroupPage = true | ComponentProps<typeof PageIconGlyph>["page"];

/**
 * What titles a related page's group, wherever the group is named — a table's
 * or list's head, a board's column or lane, the group menus: the page's own
 * icon, as Notion's sprint planning heads its current sprint (seen 2026-11-04),
 * or else a page's glyph.
 */
export function GroupPageGlyph({ page, pixels, ...marks }: {
  page: GroupPage | boolean | undefined; pixels: number;
} & { [mark: `data-${string}`]: true | undefined }) {
  if (!page) return null;
  if (page === true || !hasExplicitPageIcon(page)) return <FileBlank pixels={pixels} aria-hidden="true" {...marks} />;
  return <span aria-hidden="true" style={{ display: "inline-flex", fontSize: pixels }} {...marks}><PageIconGlyph page={page} size={pixels} /></span>;
}
