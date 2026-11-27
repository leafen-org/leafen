import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { usePageLockedForMe } from "@/lib/pageLockedForMe";

/**
 * Whether the page of a row opened over its view (a side or center peek)
 * takes changes though the view's rows don't, its page being locked. A
 * locked page stops its databases' table — cells, new rows, views and the
 * "+" of properties — but not their rows' own pages: opened over it, a row's
 * page takes edits by its own access and lock alone, its "…" menu is its
 * whole one and a locked one's "잠금" opens "잠금 해제" and "모든 사용자에
 * 대해 잠금 해제", as Notion's (seen 2026-11-16 in Chrome, a locked page's
 * inline database: its row page opened aside took a typed title and value,
 * its "…" offered 복제 · 옮기기 · 편집 제안 · 페이지 잠금 · 휴지통으로 이동,
 * and its lock menu unlocked that page alone, the page above staying
 * locked, while a cell of the table took no typing). Its database's
 * properties and page layout follow the database's own lock there, as
 * "속성 추가", 목차 and 레이아웃 사용자 지정 did (the server's
 * page_lock_database_structure.go). Anywhere else — a page
 * in the trash, a preview, one only read — the view's own read-only holds.
 */
export function useRowPageUnderPageLock({ holderPageId, readOnlyHere, held, snapshot, service }: {
  /** The page whose body holds the database. */
  holderPageId: string;
  /** What the view was given as read only, before its own reasons are added. */
  readOnlyHere: boolean;
  /** A reason the rows' pages stay read only too: a preview, the trash, no one signed in. */
  held: boolean;
  snapshot: DatabaseRowPageSnapshot | null;
  service: unknown;
}) {
  const holderLocked = usePageLockedForMe(holderPageId);
  const editsOwnPage = snapshot?.accessRole === "edit" || snapshot?.accessRole === "full_access";
  return holderLocked && readOnlyHere && !held && editsOwnPage && Boolean(service);
}
