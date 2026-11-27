import type { RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { trashMoverName } from "@/components/database/DatabaseRowTrashBanner";
import tipStyles from "@/components/IconTipButton.module.css";
import { menuFootEditedTime } from "@/lib/menuFootWords";
import { placeAround, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import type { Session } from "@/types";
import type { ShownMention } from "./mentionPreviewCard";
import styles from "./TrashedMentionTip.module.css";

/** Who sent what a mention names to the trash, and when, as the trash keeps them (mention_trash_mark.go). */
export type MentionTrashMark = { at: string; by: string };

/**
 * What a mention of a page, a database or a data source in the trash shows
 * while the pointer rests on it, in place of its card, as Notion's (seen
 * 2026-11-24 in Chrome at 1440, dpr 2, dark, two tabs — one sent there
 * itself or under a page sent there, a page block's row alike): the dark tip
 * at 14px, as wide as its words up to 300px, its start under the mention's
 * 6px below it — above it where the window's foot leaves no room — saying
 * "휴지통으로 이동한 사용자:" (a data source's "삭제한 사용자:") with the
 * mover's face 6px either side and name, and under it, lighter, when, as a
 * menu's foot says it ("오늘 오후 10:26"). It comes when a card would.
 */
export function TrashedMentionTip({ shown, mark, session, tipRef }: {
  shown: ShownMention;
  mark: MentionTrashMark;
  session: Session;
  tipRef: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation("blockItem");
  // Named again as the directory learns who moved it.
  usePeopleDirectoryRevision(Boolean(mark.by));
  const place = usePopoverPlacement(tipRef, (size, viewport) => placeAround(
    shown.anchor, size, viewport, { gap: 6, minHeight: size.height },
  ), [shown], undefined, { kind: "hovercard" });
  if (typeof document === "undefined") return null;
  const time = Date.parse(mark.at);
  const when = Number.isNaN(time) ? "" : menuFootEditedTime(time);
  const name = trashMoverName(mark.by, session.user.id, session.user.name);
  return createPortal(
    <div ref={tipRef} className={`${tipStyles.tip} ${styles.tip}`} role="tooltip" data-trashed-mention-tip={shown.id}
      style={popoverPlacementStyle(place)}>
      <span className={styles.who}>
        {t(shown.kind === "dataSource" ? "pageLink.mentionDeletedBy" : "pageLink.mentionTrashedBy")}
        <span className={styles.face}><PersonAvatar userId={mark.by || session.user.id} name={name} size={16} /></span>
        <span>{name}</span>
      </span>
      {when ? <span className={styles.when}>{when}</span> : null}
    </div>,
    document.body,
  );
}
