import { useLayoutEffect, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import styles from "./TouchDiscussionSheet.module.css";

/**
 * A block's discussion on a touch screen, as Notion's (seen 2026-11-12 in
 * iPhone Safari 402 and iPad Safari 834): pressing a line's "💬 N" or its
 * commented text raises a sheet over a shade — a whole one on a phone, a
 * half one on an iPad — its grip, "토론" in the middle of its head and a
 * round ✕ at its right, holding the block's threads as its card does (the
 * pressed text quoted), every comment's buttons showing, and "댓글 추가" at
 * its foot; the keyboard stays down. A comment's menus in it are a phone's
 * (TouchSheetLayout).
 */
export function TouchDiscussionSheet({ onClose, children }: { onClose(): void; children: ReactNode }) {
  const { t } = useTranslation("commentsPanel");
  const { phone } = useSidebarLayout(true);
  // Pressed text took the caret before the sheet came: it lets it go, so the
  // keyboard stays down and comes back neither now nor as the sheet closes.
  useLayoutEffect(() => {
    const active = document.activeElement;
    if (active instanceof HTMLElement && active.isContentEditable) active.blur();
  }, []);
  return (
    <PhoneSheet size={phone ? "full" : "half"} plain closeButton="small" title={t("discussions.title")} onClose={onClose}
      data={{ "data-touch-discussion-sheet": "" }}>
      <TouchSheetLayout><div className={styles.threads}>{children}</div></TouchSheetLayout>
    </PhoneSheet>
  );
}
