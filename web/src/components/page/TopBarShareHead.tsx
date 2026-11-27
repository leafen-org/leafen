import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { leafenDocsUrl } from "@/lib/legalLinks";
import styles from "./TopBarShareHead.module.css";

/** Where the share window's help goes: Leafen's own guide, at its share menu. */
const SHARING_GUIDE = leafenDocsUrl("guide/sharing-and-permissions.md#the-share-menu");

/**
 * The share window's head on a computer: its 공유 | 게시 tabs and, at the
 * row's end, "ⓘ 공유 기능 사용법", as Notion's (seen 2026-11-28 in Chrome at
 * 1440, dpr 1, two tabs at different times: the owner's and a guest's
 * window, on either tab), where it opens the help in a new tab — Leafen's
 * opens its own guide's chapter on sharing. The row hides with the tabs while
 * an invite is written or the advanced settings are open. A phone's sheet
 * keeps its two-part switch alone.
 */
export function TopBarShareHead({ phone, hidden, children }: {
  phone: boolean;
  /** Whether the tabs are put away. */
  hidden: boolean;
  /** The tabs. */
  children: ReactNode;
}) {
  const { t } = useTranslation("topBar");
  if (phone) return <>{children}</>;
  return (
    <div className={styles.head} style={hidden ? { display: "none" } : undefined} data-share-head="">
      {children}
      <a className={styles.help} href={SHARING_GUIDE} target="_blank" rel="noopener noreferrer" data-share-help="">
        <QuestionCircleIcon size="small" aria-hidden="true" className={styles.helpIcon} />
        {t("shareHelp")}
      </a>
    </div>
  );
}
