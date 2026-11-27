import type { Page } from "@/lib/types";
import { PageIconGlyph } from "../page/PageIcon";

/** Whether a view shows its pages' icons: it does unless turned off, as Notion's layouts (seen 2026-10-05). */
export function viewShowsPageIcons(config: { showPageIcons?: unknown }) {
  return config.showPageIcons !== false;
}

/** Whether a row has an icon of its own. */
export function rowHasOwnIcon(row: Pick<Page, "icon" | "iconType" | "notionIcon">) {
  return Boolean(row.notionIcon) || (!!row.icon && !!row.iconType && row.iconType !== "none");
}

/**
 * Whether a card shows a page icon before its title: its own, or for a page
 * with content the page-with-text icon; an empty page without one shows
 * none (Notion's board, seen 2026-10-08).
 */
export function cardShowsPageIcon(row: Pick<Page, "icon" | "iconType" | "notionIcon" | "hasContent">) {
  return rowHasOwnIcon(row) || Boolean(row.hasContent);
}

/**
 * Whether a timeline's bar shows a page icon before its title: its own
 * alone, a page with content and no icon showing none (Notion's, seen
 * 2026-11-15 at a week's zoom: "🍎 행 23" beside a plain "행 32" with a body).
 */
export function barShowsPageIcon(row: Pick<Page, "icon" | "iconType" | "notionIcon">) {
  return rowHasOwnIcon(row);
}

/**
 * A card's page icon before its title, as Notion's board, gallery and
 * calendar cards show it while "페이지 아이콘 표시" is on (seen 2026-10-05,
 * 2026-10-08, 2026-11-15): its own icon, or a page with content's; a
 * timeline's bar (`ownOnly`) its own alone (barShowsPageIcon).
 */
export function CardTitleIcon({ row, show, size = 16, className, ownOnly = false }: {
  row: Pick<Page, "icon" | "iconType" | "workspaceId" | "kind" | "notionIcon" | "hasContent">;
  show: boolean;
  size?: number;
  className?: string;
  ownOnly?: boolean;
}) {
  if (!show || !(ownOnly ? barShowsPageIcon(row) : cardShowsPageIcon(row))) return null;
  return (
    <span className={className} aria-hidden="true" data-card-title-icon>
      <PageIconGlyph page={{
        icon: row.icon ?? "", iconType: row.iconType, workspaceId: row.workspaceId, kind: row.kind,
        notionIcon: row.notionIcon, hasContent: row.hasContent,
      }} size={size} />
    </span>
  );
}
