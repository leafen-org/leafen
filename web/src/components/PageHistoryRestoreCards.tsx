import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import { PageIconGlyph } from "@/components/page/PageIcon";
import type { Page } from "@/types";
import styles from "./PageHistoryRestorePanel.module.css";

type CardPage = Pick<Page, "icon" | "iconType"> & { workspaceId?: string };
type Card = { page: CardPage; title: string; time: string };

/**
 * The picture above "이 버전 복원", as Notion's (measured 2026-11-12): four
 * 166×123 cards lying back in perspective, 8px apart — the page as it is now
 * in front, the version to restore three times behind it — and the version
 * to restore flat on top, called "현재 버전". Over and over it fades off,
 * leaving the page as it is now to be called so, slides out from behind the
 * stack to the right, rises and slides back in on top, and takes the name
 * again (PageHistoryRestorePanel.module.css). It only decorates; nothing in
 * it answers the pointer, and it stands still for reduced motion.
 */
export function PageHistoryRestoreCards({ current, version }: { current: Card; version: Card }) {
  const { t } = useTranslation("updatesPanel");
  const back = [current, version, version, version];
  const tag = t("versions.currentVersion");
  return (
    <div className={styles.historyRestoreCards} aria-hidden="true" data-page-history-restore-cards="true">
      <div className={styles.historyRestoreCardStack}>
        {back.map((card, index) => (
          <RestoreCard key={index} card={card} tag={tag} place={index === 0 ? "now" : "back"}
            style={{ "--restore-card-offset": `${(index + 1) * 8}px`, zIndex: back.length - index } as CSSProperties} />
        ))}
        <RestoreCard card={version} tag={tag} place="restored" />
      </div>
    </div>
  );
}

function RestoreCard({ card, tag, place, style }: { card: Card; tag: string; place: "now" | "back" | "restored"; style?: CSSProperties }) {
  return (
    <div className={styles.historyRestoreCard} data-place={place} style={style}>
      <span className={styles.historyRestoreCardTag}>{tag}</span>
      <span className={styles.historyRestoreCardIcon}>
        {card.page.icon ? <PageIconGlyph page={card.page} size={20} fallback="" /> : null}
      </span>
      <span className={styles.historyRestoreCardHead}>
        <span className={styles.historyRestoreCardTime}>{card.time}</span>
        <span className={styles.historyRestoreCardTitle}>{card.title}</span>
      </span>
      <span className={styles.historyRestoreCardLines}>
        <span /><span /><span />
      </span>
    </div>
  );
}
