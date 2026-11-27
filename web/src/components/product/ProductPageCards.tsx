import { DesktopTabOverlays } from "@/components/desktop/DesktopTabOverlays";
import { DateMentionTip } from "@/components/editor/DateMentionTip";
import { LinkMentionPreview } from "@/components/editor/LinkMentionPreview";
import { PageMentionPreview } from "@/components/editor/PageMentionPreview";
import { inDesktopTab } from "@/lib/desktopTabs";
import type { Page, Session } from "@/types";

/**
 * The cards and menus drawn over the product's pages: a page mention's card,
 * an outside page's mention's card, a date mention's day in full,
 * and in the Mac app's tabs what the shown tab draws for its strip — a tab's
 * card, the history menu and the groups' menu (DesktopTabOverlays).
 */
export function ProductPageCards({ pages, session, workspaceId }: { pages: Page[]; session: Session; workspaceId: string }) {
  return <>
    <PageMentionPreview pages={pages} session={session} workspaceId={workspaceId} />
    <LinkMentionPreview />
    <DateMentionTip />
    {inDesktopTab() ? <DesktopTabOverlays pages={pages} session={session} workspaceId={workspaceId} /> : null}
  </>;
}
