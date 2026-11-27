import { useEffect, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";

import { request } from "@/apiTransport";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { usePageActionAccess, usePageActionAccessReader } from "@/lib/pageActionAccessContext";
import { endPageCopy, pageAccessTopic, pageCopyAsked, pageCopyDone, pageCopyUnderWay, setPageCopyProgress, useAnyPageCopy } from "@/lib/pageCopyProgress";
import { useOptionalProductToast } from "@/lib/productToast";
import type { Session } from "@/types";
import type { PageActionAccess } from "@/workspaceMembershipWire.generated";

import styles from "./PageCopying.module.css";

/** Where the bar names a page's last edit, a copy being made counts, as Notion's does. */
export function PageCopyPercent({ percent }: { percent: number }) {
  const { t } = useTranslation("topBar");
  if (percent === pageCopyDone) {
    return (
      <span className={styles.percent} data-page-copy-percent="done" role="status">
        <svg className={styles.check} viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8.5 6.5 12 13 4.5" /></svg>
        {t("copied")}
      </span>
    );
  }
  return (
    <span className={styles.percent} data-page-copy-percent>
      {t("copying")}
      <span className={styles.spinner} aria-hidden="true" />
      {percent}%
    </span>
  );
}

const COPY_CHECK_MS = 2_500;

/**
 * The top of a copy on screen, however it was opened (a reload, another tab,
 * another person): the count goes on from what its page says, and the pages
 * done so far come into the list.
 */
export function PageCopyFollow({ session, pageId, onProgress }: { session: Session; pageId: string; onProgress(): void }) {
  usePageActionAccess(pageId, true);
  const access = usePageActionAccessReader();
  const known = useSyncExternalStore(access.subscribe, () => access.copyPercent(pageId));
  const underWay = known !== undefined;
  useEffect(() => {
    if (!underWay) return undefined;
    let stopped = false;
    let shown = access.copyPercent(pageId) ?? 0;
    setPageCopyProgress(pageId, shown);
    // One of the person's tabs in sight asks for all of them, and a tab
    // behind asks nothing (followAcrossTabs).
    const topic = pageAccessTopic(session.user.id, pageId);
    const stop = followAcrossTabs(topic, COPY_CHECK_MS,
      () => request<PageActionAccess>(`/pages/${encodeURIComponent(pageId)}?accessOnly=1`, {}, session).then((read) => tellTabs(topic, read)),
      (value) => {
        const read = value as PageActionAccess | undefined;
        if (stopped || !read) return;
        if (read.copyPercent === undefined) {
          stopped = true;
          stop();
          endPageCopy(pageId);
          onProgress();
        } else if (read.copyPercent !== shown) {
          shown = read.copyPercent;
          setPageCopyProgress(pageId, shown);
          onProgress();
        }
      });
    return () => {
      stopped = true;
      stop();
      // Left behind, the count is read anew if the page is opened again.
      if (!pageCopyAsked(pageId) && pageCopyUnderWay(pageId)) setPageCopyProgress(pageId, null);
    };
    // The callback may change with each render; the copy is what this follows.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [access, pageId, session, underWay]);
  return null;
}

const NOTICE_AFTER_MS = 2_000;

/** A copy takes minutes when big: past the moment one expects, it says so until it ends, as Notion's (seen 2026-11-05). */
export function PageCopyNotice() {
  const { t } = useTranslation("moveToDialog");
  const toast = useOptionalProductToast();
  const copying = useAnyPageCopy();
  useEffect(() => {
    if (!copying || !toast) return undefined;
    const progress = t("copy.progress");
    const timer = window.setTimeout(() => toast.notify(progress, "default",
      { label: t("copy.dismiss"), onClick: () => {}, autoDismissMs: null, dismissible: false }, "progress"), NOTICE_AFTER_MS);
    return () => {
      window.clearTimeout(timer);
      toast.dismissMessage(progress);
    };
    // Once for each stretch of copying.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [copying]);
  return null;
}
