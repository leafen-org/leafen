import { Suspense, useEffect, useState } from "react";
import { request } from "@/apiTransport";
import { isGuestMFARequired } from "@/lib/guestMFARequired";
import { lazyScreen } from "@/lib/lazyScreen";
import { StandaloneRowPageScreen as StandaloneRowPage } from "../database/standaloneRowPageScreen";
import type { Page, Session } from "@/types";
import { GuestMFARequired } from "./GuestMFARequired";
import { WikiPageMenu, type WikiPageMenuActions } from "./WikiPageMenu";

const MoveToDialog = lazyScreen(() => import("../MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog })), { typedOnOpen: true });

/**
 * A page of a wiki is its wiki database's page: it opens with the wiki's
 * properties and its body, as a database page does, and its "⋯" is the
 * page menu (WikiPageMenu), as Notion's wiki pages (seen 2026-10-13);
 * Cmd/Ctrl+Shift+P opens its "옮기기" (seen 2026-11-14). Its "업데이트와
 * 애널리틱스" docks the updates beside it, the same panel its bar's edit time
 * opens, as Notion's wiki page (seen 2026-11-17, Chrome 1440). Its graph and
 * room are its database page's body's, the page's one room (lib/pageDrawnAsRow.ts).
 */
export function WikiPageView({ page, pages, session, organizationId, favorite, onClose, menu }: {
  page: Page;
  pages: Page[];
  session: Session;
  organizationId?: string | null;
  favorite: boolean;
  onClose: () => void;
  menu: WikiPageMenuActions;
}) {
  const { onPageChange } = menu;
  return (
    <Suspense fallback={null}>
      <StandaloneRowPage session={session} rowId={page.id} organizationId={organizationId} fallback={<WikiPageUnread pageId={page.id} session={session} />} onClose={onClose}
        pageMenu={(anchor, close, { canEdit, suggesting, style, openUpdates }) => (
          <WikiPageMenu page={page} pages={pages} session={session} favorite={favorite} menu={menu} anchor={anchor}
            canEdit={canEdit} suggesting={suggesting} style={style} onOpenUpdatesPanel={openUpdates} onClose={close} />
        )}
        pageMove={onPageChange ? (anchor, close) => (
          <Suspense fallback={null}>
            <MoveToDialog pageId={page.id} pages={pages} session={session} anchor={anchor} anchorGap={0}
              onClose={close} onPageChange={onPageChange} onStatus={menu.onStatus} />
          </Suspense>
        ) : undefined} />
    </Suspense>
  );
}

/**
 * A wiki's page its database's page couldn't read: a guest whose organization
 * asks for 2-step verification first is shown that screen, as on any page
 * (GuestMFARequired) — the database page's read says only that it isn't there.
 */
function WikiPageUnread({ pageId, session }: { pageId: string; session: Session }) {
  const [guestMFA, setGuestMFA] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    void request(`/pages/${encodeURIComponent(pageId)}`, { signal: controller.signal }, session)
      .catch((error: unknown) => { if (!controller.signal.aborted && isGuestMFARequired(error)) setGuestMFA(true); });
    return () => controller.abort();
  }, [pageId, session]);
  return guestMFA ? <GuestMFARequired session={session} /> : null;
}
