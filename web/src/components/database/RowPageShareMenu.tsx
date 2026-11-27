import { pageHref } from "@/lib/navigation";
import { serverUrl } from "@/lib/serverOrigin";
import type { SharePage } from "../page/topBarShareModel";
import { useId, type RefObject } from "react";
import type { Session } from "@/types";
import { TopBarShare } from "../page/TopBarShare";

/**
 * Row pages use the common sharing flow and native page grants. It opens at
 * once with what was last heard of the row page's sharing, and its own read
 * says whether this person changes it or reads it as words, and fails as any
 * page's share window does (2026-11-16: a read of the row page's permissions
 * first held the window empty for a round trip before it could open).
 */
export function RowPageShareMenu({ session, page, containerPageId, anchorRef, invite = "", onClose }: {
  session: Session; page: SharePage; containerPageId: string; onClose(): void;
  /** The row page's "공유", the menu hanging under it however it was asked for. */
  anchorRef: RefObject<HTMLElement | null>;
  /** A name or email ready to be invited, as an "@" invite asks (pageShareEvents.ts). */
  invite?: string;
}) {
  const id = useId();
  return <TopBarShare key={invite} id={id} anchorRef={anchorRef} page={page} pages={[page]} canRequestEdit={false} initialInvite={invite} pageUrl={serverUrl(pageHref(containerPageId,new URLSearchParams({p:page.id,pm:"f"})))} session={session} onClose={onClose} />;
}
