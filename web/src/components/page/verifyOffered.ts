import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { useSidebarLayout } from "@/components/shell/AppShell";

/**
 * Whether a head offers "인증 추가" — a page's, a full-page database's, an
 * inline database opened alone, a database page's — as Notion's does: not on
 * a touch screen (seen 2026-10-29 on a phone and 2026-11-12 on an iPad: "아이콘
 * 추가 · 커버 추가 · 댓글 추가"), and not to the workspace's guest, who may edit
 * what is shared with them but verifies nothing (seen 2026-11-18 in Chrome at
 * 1440, two tabs, the second test account a guest: a page's head "아이콘 추가 ·
 * 커버 추가 · 댓글 추가", a full-page database's "아이콘 추가 · 커버 추가 · 설명
 * 추가" with a row's side peek open or not, its row page's "아이콘 추가 · 커버
 * 추가 · 레이아웃 사용자 지정"; a member's keeps "인증 추가" at 1440, 1024, 768
 * and 640, with a side peek open or not — no width drops it).
 * A head that doesn't say its workspace is the reader's open one's.
 */
export function useVerifyOffered(workspaceId?: string): boolean {
  const { touch } = useSidebarLayout(true);
  const guest = useProductPagesSelect((pages) => {
    const reading = workspaceId ?? pages?.workspaceId;
    return pages?.workspaces?.find(({ id }) => id === reading)?.guest === true;
  });
  return !touch && !guest;
}
