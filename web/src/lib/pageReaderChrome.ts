import { useProductPagesSelect } from "@/components/product/productPagesStore";

/**
 * What a page's bar and "⋯" offer one who may only read it or comment on it,
 * one rule for a page (TopBar, RowMenuActions), a database row's page opened
 * whole and its peek (DatabaseRowPageActions, useDatabaseRowPageMenu), as
 * Notion's (a page seen 2026-10-08; a row's page and its peek seen
 * 2026-11-28 in Chrome at 1440, dpr 1, two tabs at different times, as a
 * guest given read and comment on the page above). Who opens the page's
 * updates is opensPageUpdates' own rule.
 */

/**
 * Whether the bar offers the page's share and its link: not to a guest who
 * may not edit the page, whose bar keeps its edited time, ☆ and "⋯" alone — a
 * page's, a row's page opened whole ("편집 시각 · ☆ · ⋯") and its peek ("☆ ·
 * ⋯"); a member reading it keeps them, and so does a guest who edits.
 */
export function barOffersShare(canEdit: boolean, guest: boolean): boolean {
  return canEdit || !guest;
}

/** Whether the reader is a guest of the workspace `workspaceId`. */
export function useWorkspaceGuest(workspaceId: string | undefined): boolean {
  return useProductPagesSelect((pages) => pages?.workspaces?.find(({ id }) => id === workspaceId)?.guest === true);
}

/**
 * The rows of a page's "⋯" one who may not edit it is offered, the others
 * left out rather than dimmed: "링크 복사", "페이지 내용 복사하기" (dimmed
 * for a guest), "편집 제안" to one who may comment, "알림 받기" and "더
 * 보기" with "프레젠테이션 모드" and "내보내기" (dimmed) — and no edited foot
 * under them (seen on a row's page, opened whole and in its peek, as on a
 * page: "링크 복사 · 페이지 내용 복사하기 · 알림 받기 · 더 보기" to a read
 * guest, "편집 제안" before "알림 받기" to a comment guest).
 */
const READER_ROWS = new Set(["link", "contents", "suggest", "notify", "more", "present", "export", "analytics"]);

export type PageMenuRowKey = "link" | "contents" | "send" | "duplicate" | "duplicate-private" | "move" | "merge-csv" | "history" | "analytics"
  | "theme" | "suggest" | "notify" | "lock" | "archive" | "trash" | "more" | "present" | "undo" | "import" | "export" | "read-log"
  | "connections" | "wiki";

/** Whether the page's "⋯" offers `row` to its reader: every row to one who may edit, the reader's rows alone otherwise. */
export function pageMenuOffers(row: PageMenuRowKey, canEdit: boolean): boolean {
  return canEdit || READER_ROWS.has(row);
}

/** Whether the page's "⋯" ends with when and by whom it was last edited: only for one who may edit it. */
export function pageMenuShowsEditedFoot(canEdit: boolean): boolean {
  return canEdit;
}

/**
 * Whether one who may only read or comment on a page is offered to ask for
 * edit access — in the share window's band alone ("나만 이 페이지를 볼 수
 * 있습니다." or "…댓글을 달 수 있습니다." and "편집 권한 요청"), never in
 * the bar nor its "⋯": Notion's bar stays "편집 시각 · 공유 · 링크 · ☆ · ⋯"
 * and its "⋯" has no such row (seen 2026-11-28 in Chrome at 1440, dpr 1: the
 * workspace's owner given read, and comment, on a page shared from its test
 * page, the band in the window each time; a member given read on another
 * member's private page, two tabs, no request in the bar). The workspace's own
 * members ask; a guest, a restricted member and a reader of an app's
 * workspace aren't offered it (seen 2026-11-07: no band in the window).
 */
export function shareAsksEdit({ guest, restricted, appManaged }: { guest: boolean; restricted: boolean; appManaged: boolean }): boolean {
  return !appManaged && !guest && !restricted;
}

/**
 * Whether "페이지 내용 복사하기" and "내보내기" are offered (not dimmed) to
 * whoever reads the page: to the workspace's own people whatever they may do
 * on it — a member given read or comment copies its Markdown and exports it —
 * and dimmed for a guest, one allowed to edit too, as Notion's (seen
 * 2026-11-28 in Chrome at 1440, dpr 1, two tabs at different times: the
 * workspace's owner given read and comment had both, the copy taking the
 * body's Markdown; a guest given edit, comment or read had them dimmed). The
 * server reads a page's Markdown and export for anyone who may read it.
 */
export function copiesPageContents(guest: boolean): boolean {
  return !guest;
}
