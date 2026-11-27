import { type RefObject, useState } from "react";
import { useTranslation } from "react-i18next";
import { FootActivityPanel, footActivityRows, useFootActivityHover } from "@/components/RowMenuEditedFoot";
import { pageEditedActivity, usePageActivityShown, usePageLastEdit, usePageLastEditVia } from "@/lib/pageEditedActivity";
import type { Page, Session } from "@/types";

/**
 * What the bar's edited time tells of when the pointer rests on it: the page
 * (a row's page, a database opened alone) as its bar has it — read again from
 * the server as the pointer comes where `fresh`, as a page's menu foot reads
 * it (usePageLastEdit), an edit made elsewhere reaching the open text but not
 * the page's own fields.
 */
export type TopBarActivitySource = {
  session: Session;
  page: Pick<Page, "id" | "workspaceId" | "updatedAt" | "lastEditedBy" | "createdBy" | "createdAt">;
  fresh?: boolean;
};

/**
 * The bar's edited time's "활동" card, as Notion's (seen 2026-11-28 in Chrome
 * at 1440, dpr 1, two tabs at different times): the card a page menu's foot
 * shows beside it (FootActivityPanel) — "활동", then "편집자: 〈name〉" and
 * "생성자 〈name〉" with faces and how long ago — under the time instead,
 * 6px below it from its start; a quarter of a second after the pointer comes
 * to the time (232–241 ms, five times) and half a second after it has left
 * the time and the card both (528 ms), the pointer free to go into it. The
 * workspace's own people see it, its guests never (pageActivityShown); the
 * time's press is the updates' own (opensPageUpdates). `hover` goes on the
 * time; the card reads who made and edited the page from the pointer's first
 * coming, so the names are there when it shows.
 */
export function useTopBarActivityCard(source: TopBarActivitySource | undefined, anchor: RefObject<HTMLElement | null>) {
  const shown = usePageActivityShown(source?.page.workspaceId) ? source : undefined;
  const { open, pointed } = useFootActivityHover(Boolean(shown));
  const [warm, setWarm] = useState(false);
  const hover = shown ? {
    onMouseEnter: () => { setWarm(true); pointed(true); },
    onMouseLeave: () => pointed(false),
  } : {};
  const card = shown && warm ? <TopBarActivityCard source={shown} anchor={anchor} open={open} pointed={pointed} /> : null;
  return { hover, card };
}

function TopBarActivityCard({ source, anchor, open, pointed }: {
  source: TopBarActivitySource; anchor: RefObject<HTMLElement | null>; open: boolean; pointed: (on: boolean) => void;
}) {
  const { t } = useTranslation("blockHandle");
  const { session, page } = source;
  const edited = usePageLastEdit(session, page, Boolean(source.fresh));
  const activity = pageEditedActivity({ ...page, ...edited }, session.user.id);
  const via = usePageLastEditVia(session, page.id, activity.editor.id, true);
  if (!open) return null;
  return <FootActivityPanel foot={anchor} head={t("footer.activity")} under pointed={pointed}
    rows={footActivityRows({ editor: t("footer.activityEditor"), creator: t("footer.activityCreator") }, activity, via)} />;
}
