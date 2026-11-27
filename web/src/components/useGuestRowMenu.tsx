import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { PUBLIC_CONTENT_CHANGED_EVENT } from "@/components/page/pageShareEvents";
import { useGuestEditsPageAbove } from "@/lib/pageActionAccessContext";
import { useProductToast } from "@/lib/productToast";
import type { Page, Session } from "@/types";
import { type GuestCopyChoice, useGuestDuplicateToWorkspace } from "./useGuestDuplicateToWorkspace";

type GuestRowMenuInput = {
  session: Session;
  page: Page;
  pages: readonly Page[];
  /** Whether the reader is a guest in the page's workspace. */
  guest: boolean;
  /** Whether they edit the page, and whether they have its full access (usePageFullAccess). */
  canEdit: boolean;
  fullAccess: boolean | null;
  busy: boolean;
  /** The favorites' copy of the page. */
  favoritesList: boolean;
  close(): void;
};

/**
 * The rows a guest's sidebar menus (RowMenuSidebarActions, and the phone's
 * sheet) show, by their access to the page and to the page above it, as
 * Notion's (seen 2026-11-16 in Chrome, two tabs at different times, a guest
 * given each access to a page topping what is shared with them and to pages
 * under one they edit): "복제" under a page above they edit; with full access
 * over none they edit, "복제 대상 ›" their own workspaces instead; "옮기기"
 * lit only with full access under a page they edit; "휴지통으로 이동" lit
 * under a page they edit; "위키로 전환" with full access. Reading or
 * commenting, the foot "이 블록은 제한되어 있어…" shows only on a page whose
 * access is cut below the page above's, one under a page they edit.
 */
export type GuestSidebarRows = {
  duplicate: boolean;
  /** Where "복제 대상 ›" copies the page, where it stands for "복제": the guest's own workspaces, or with none a new one. */
  duplicateTo?: readonly GuestCopyChoice[];
  move: boolean;
  trash: boolean;
  wiki: boolean;
  restrictedFoot: boolean;
};

/**
 * What a guest's page menus (RowMenu) do as Notion's guest's: copy and trash
 * follow whether they edit the page above (useGuestEditsPageAbove), "페이지
 * 복제 ›" copies into their own workspaces (useGuestDuplicateToWorkspace),
 * the page topping what is shared with them has "나가기" (useGuestLeavePage),
 * and their sidebar menus' rows follow their access (GuestSidebarRows).
 */
export function useGuestRowMenu({ session, page, pages, guest, canEdit, fullAccess, busy, favoritesList, close }: GuestRowMenuInput) {
  const editsAbove = useGuestEditsPageAbove(page.parentId, guest);
  const copy = useGuestDuplicateToWorkspace(session, page, busy, close);
  const leave = useGuestLeavePage({ session, page, pages, guest, favoritesList, close });
  const full = canEdit && fullAccess === true;
  const rows: GuestSidebarRows | undefined = guest ? {
    duplicate: canEdit && editsAbove,
    duplicateTo: full && !editsAbove ? copy.groups.find((places) => places.length > 0) : undefined,
    move: full && editsAbove,
    trash: canEdit && editsAbove,
    wiki: full,
    restrictedFoot: !canEdit && editsAbove,
  } : undefined;
  return { editsAbove, copy, leave, rows };
}

/**
 * A guest's "나가기" on a page topping what is shared with them, as Notion's
 * (seen 2026-11-16 in Chrome, two tabs at different times, a guest with full
 * access in a free workspace): beside "즐겨찾기에 추가" in its sidebar row's
 * menu. It asks nothing: the menu closes at the press, and once the server has
 * taken the guest's own share away (POST /pages/{id}/leave) the page leaves
 * their sidebar while the page they read stays. From the page itself, or a page
 * under it, they stay at its address, where it reads as any page they can't
 * open (PageNoAccess, as a page whose access was taken away while open):
 * Notion stays there too, its window empty until reloaded, then the same card
 * (seen 2026-11-16 by a guest with pages left; by one with none, empty under
 * its guest band). The row is the server's to refuse first, as a sharing change is; a
 * refusal says so. undefined where the menu has no such row: a member's, a
 * favorite's copy and a page under one the guest also opens.
 */
function useGuestLeavePage({ session, page, pages, guest, favoritesList, close }: Pick<GuestRowMenuInput, "session" | "page" | "pages" | "guest" | "favoritesList" | "close">): (() => void) | undefined {
  const { t } = useTranslation("rowMenu");
  const { notify } = useProductToast();
  const top = guest && !favoritesList && !pages.some((other) => other.id === page.parentId);
  if (!top) return undefined;
  return () => {
    close();
    void (async () => {
      try {
        await request<void>(`/pages/${encodeURIComponent(page.id)}/leave`, { method: "POST" }, session);
      } catch {
        notify(t("toast.couldntLeavePage"), "error");
        return;
      }
      // Who has a page decides where the sidebar shows it, so the list is read again.
      window.dispatchEvent(new Event(PUBLIC_CONTENT_CHANGED_EVENT));
    })();
  };
}
