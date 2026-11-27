import { useRef, useState, type CSSProperties, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import rowMenuStyles from "@/components/RowMenu.module.css";
import { PageActionSearch } from "@/components/PageActionSearch";
import { pageMenuItem, usePageMenu, type PageMenuEntry, type PageMenuSubmenu } from "@/components/PageMenu";
import { RowMenuConnectionsSubmenu } from "@/components/RowMenuConnections";
import { RowMenuEditedFoot } from "@/components/RowMenuEditedFoot";
import { usePageConnections } from "@/components/RowMenuSubmenus";
import { ConnectionsIcon, LinkIcon } from "@/icons/leafen";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { NativeActionMenu, NativeMenuHeader, NativeMenuSection } from "@/lib/nativeActionMenu";
import { usePageLastEdit, usePageLastEditVia } from "@/lib/pageEditedActivity";
import { editorName } from "@/lib/peopleDirectory";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Session } from "@/types";

/**
 * People's "⋯" menu, as Notion's People page's (seen 2026-11-16 at 1440 in
 * two tabs at two times: 1172–1428, 256 wide, from 44): "작업 검색..." with
 * the caret in it, "링크 복사 ⌘⌥L", under a line "연결 ›" with how many
 * the page has ("없음") and its 350-wide box beside it, as any page's
 * (RowMenuConnectionsSubmenu) — a connected app reads People as a member
 * does (people_database_access.go) — and under another who edited People
 * last and when, as a database page's menu ends (RowMenuEditedFoot): the
 * owner who changed it, or Leafen, which made it (Notion's People names its
 * owner who changed it, seen 2026-11-16 in two tabs). Notion's "Mac 앱에서
 * 열기" is the open-in-app band's and its "데이터베이스 사용법" opens
 * Notion's help center, both left out as a database page's menu leaves them.
 */
export function PeopleDatabaseMenu({ anchor, session, workspaceId, pageId, updatedAt, lastEditedBy, title, onCopyLink, onClose }: {
  anchor: RefObject<HTMLButtonElement | null>; session: Session; workspaceId: string; pageId: string; updatedAt: string;
  lastEditedBy: string; title: string; onCopyLink(): void; onClose(): void;
}) {
  const { t } = useTranslation(["topBar", "rowMenu"]);
  const ref = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const pageMenu = usePageMenu(query);
  const edited = usePageLastEdit(session, { id: pageId, updatedAt, lastEditedBy }, true);
  const connections = usePageConnections({ id: pageId, workspaceId }, session, true);
  const editorId = edited.lastEditedBy || null;
  const via = usePageLastEditVia(session, pageId, editorId, true);
  const rect = popoverRectOf(anchor.current);
  const place = usePopoverPlacement(ref, rect ? (size, viewport) => placeAround(rect, size, viewport, { gap: 8, align: "end", keepSide: true }) : null,
    [anchor], undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose, initialFocus: searchRef });
  const item = pageMenuItem(false);
  const groups: PageMenuEntry[][] = [[
    item(pageMenu.matches, "link", t("rowMenu:actions.copyLink"), <LinkIcon size="regular" aria-hidden="true" />, onCopyLink,
      { hint: shortcutCompactLabel("copyPageLink") }),
  ], [connections.manageable ? {
    submenu: "connections", label: t("rowMenu:actions.connections"), icon: <ConnectionsIcon size="regular" aria-hidden="true" />, width: 350,
    value: connections.value, disabled: !connections.loaded, data: { "data-page-connections-action": "" },
    panel: (row, close) => <RowMenuConnectionsSubmenu row={row} connections={connections} onClose={close} />,
  } satisfies PageMenuSubmenu : null]];
  return createPortal(
    <>
      <button type="button" className={rowMenuStyles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
        aria-label={t("rowMenu:aria.closePageActions")} onClick={onClose} />
      <div ref={ref} className={`${rowMenuStyles.menu} ${rowMenuStyles.hangingMenu}`} data-people-actions-menu onPointerMove={pageMenu.onPointerMove}
        style={{ width: 256, ...(place ? popoverPlacementStyle(place) : { visibility: "hidden" }) } as CSSProperties}>
        <NativeActionMenu aria-label={t("topBar:moreActions", { pageTitle: title })}
          renderEmptyState={() => <p className={rowMenuStyles.menuEmpty}>{t("rowMenu:noResults")}</p>}>
          <NativeMenuSection aria-label={t("rowMenu:searchActions")}><NativeMenuHeader>
            <PageActionSearch className={rowMenuStyles.actionSearch} label={t("rowMenu:searchPageActions")} value={query} inputRef={searchRef}
              marker="people-menu-search" onChange={setQuery} />
          </NativeMenuHeader></NativeMenuSection>
          {pageMenu.list(groups)}
        </NativeActionMenu>
        {/* A search lists rows alone, as a page's menu. */}
        {query.trim() ? null : <RowMenuEditedFoot name={editorId ? editorName(editorId, session.user.id) : ""} editorId={editorId}
          via={via} at={edited.updatedAt ?? updatedAt} marker="people-menu-edited" />}
      </div>
      {pageMenu.overlay(groups)}
    </>,
    document.body,
  );
}
