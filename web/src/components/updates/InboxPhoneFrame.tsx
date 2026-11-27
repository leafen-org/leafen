import type { ReactNode, Ref } from "react";
import { useTranslation } from "react-i18next";
import { MailIcon } from "@/icons/leafen";
import { WorkspaceIconGlyph, workspaceIconLetter } from "../page/PageIcon";
import { PhoneFoot } from "../shell/PhoneFoot";
import styles from "./InboxPhoneFrame.module.css";

// A phone's inbox in the Notion Android app's frame (measured 2026-11-21 on
// the A90 at 411, light, twice: with a notification and empty; phones follow
// the app — the phone basis decision): the sidebar's warm ground, a top bar of
// the workspace's icon and the inbox's tab, the list's head with its filter
// and "⋯" on the first group's line, rows to the screen's edge, and round
// search and new page buttons at the foot. The app's other tabs (home, chat,
// meetings) and its "AI에게 질문하기" bar have no place here.

/**
 * The top bar: the workspace's icon, a 40 circle in a white ring 16 in, which
 * goes back to the sidebar as the app's opens it; the inbox's tab where the
 * app's stands last, 75 wide and 20 from the edge, pressed, its tray filled
 * with a red dot while something is unread. The tab is the inbox's as the
 * drawer's tab bar has it: pressed again it closes the inbox, back to the
 * drawer (2026-11-27, the user's hand test on a Galaxy Fold's outer screen:
 * drawn as a mark, it took the press and did nothing; the app's own press on
 * its tab not seen — its inbox is a panel of real pages).
 */
export function InboxPhoneBar({ closeRef, onClose, unread, workspaceIcon, workspaceName }: {
  closeRef: Ref<HTMLButtonElement>;
  onClose(): void;
  unread: boolean;
  workspaceIcon?: string;
  workspaceName: string;
}) {
  const { t } = useTranslation("updatesPanel");
  const letter = workspaceIconLetter(workspaceName);
  return <div className={styles.bar} data-inbox-phone-bar>
    <button ref={closeRef} type="button" className={styles.workspace} data-inbox-close
      aria-label={t("inbox.close")} title={t("inbox.close")} onClick={onClose}>
      <span className={styles.workspaceIcon} data-letter={workspaceIcon?.trim() ? undefined : "true"} aria-hidden="true">
        {workspaceIcon?.trim() ? <WorkspaceIconGlyph icon={workspaceIcon} size={32} fallback={letter} /> : letter}
      </span>
    </button>
    <button type="button" className={styles.tab} data-inbox-phone-tab aria-pressed="true" aria-label={t("inbox.titles.inbox")} onClick={onClose}>
      <MailIcon size="regular" weight="fill" aria-hidden="true" />
      {unread ? <span className={styles.unread} data-inbox-phone-unread aria-hidden="true" /> : null}
    </button>
  </div>;
}

/** The foot: what the sidebar hands it (search and a new page), over the list's end, as a page's (PhoneFoot). */
export function InboxPhoneFoot({ children }: { children: ReactNode }) {
  return <PhoneFoot place="inbox">{children}</PhoneFoot>;
}

/**
 * An empty inbox, as the app's (measured 2026-11-21 on the A90 at 411): a blue
 * ✓ over its words, a line and a note under it, in the screen's middle.
 */
export function InboxPhoneEmpty({ title, note }: { title: string; note: string }) {
  return <div className={styles.empty} role="status" data-inbox-phone-empty>
    <svg className={styles.emptyMark} viewBox="0 0 22.5 22.5" aria-hidden="true"><path d="M1.2 12 9.6 20.6 21.4 1.6" /></svg>
    <span className={styles.emptyWords}>{title}</span>
    <span className={styles.emptyWords}>{note}</span>
  </div>;
}
