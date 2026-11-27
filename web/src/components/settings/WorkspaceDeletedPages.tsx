import { api } from "@/api";
import { restoreDatabaseRowPage } from "@/databaseRowPageApi";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { newId } from "@/lib/ids";
import { isLegalHoldPermanentDeleteError } from "@/lib/nativeTrashLifecycle";
import { pageMetadataBaseline, sendKeptPageMetadata } from "@/lib/nativePageMetadata";
import { useProductToast } from "@/lib/productToast";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import type { DeletedPage } from "@/pagePermanentDeleteWire.generated";
import type { Session } from "@/types";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ListNotice } from "./SettingsRowNotice";
import styles from "./WorkspaceDeletedPages.module.css";

function formatDay(at: number, language: string) {
  return new Intl.DateTimeFormat(language, { dateStyle: "long" }).format(at);
}

/**
 * 보안 › 데이터 보존's list of the pages deleted for good that wait their 30
 * days (the page trash decision's 2026-09-30 addition): only the workspace's
 * owner sees them, and brings one back or deletes it at once.
 */
export function WorkspaceDeletedPages(props: { session: Session; workspaceId: string }) {
  const { session, workspaceId } = props;
  const { t, i18n } = useTranslation("workspaceSettingsDialog");
  const { notify } = useProductToast();
  const [pages, setPages] = useState<DeletedPage[] | null>(null);
  // A failed read, said where the list shows with a way to read it again,
  // never as no pages kept.
  const [readFailed, setReadFailed] = useState(false);
  const [readTurn, setReadTurn] = useState(0);
  const [busy, setBusy] = useState(false);

  // The list comes a window at a time, newest first, each shown as it comes,
  // and goes on to the oldest.
  useEffect(() => {
    const controller = new AbortController();
    setPages(null);
    setReadFailed(false);
    void (async () => {
      let cursor = "";
      do {
        const response = await api.listDeletedPages(session, workspaceId, cursor, controller.signal);
        if (controller.signal.aborted) return;
        cursor = response.nextCursor;
        // A window may come empty with more to come (the server reads so far
        // a request past pages the owner doesn't read): the list stays
        // loading until a line or its end comes, never "none" meanwhile.
        if (response.pages.length === 0 && cursor) continue;
        // Added to what is shown, so one brought back or deleted meanwhile stays gone.
        setPages((current) => {
          const shown = new Set((current ?? []).map((page) => page.pageId));
          return [...(current ?? []), ...response.pages.filter((page) => !shown.has(page.pageId))];
        });
      } while (cursor);
    })().catch(() => {
      if (!controller.signal.aborted) setReadFailed(true);
    });
    return () => controller.abort();
  }, [readTurn, session, workspaceId]);

  const leave = (pageId: string) => setPages((current) => (current ?? []).filter((page) => page.pageId !== pageId));

  async function recover(page: DeletedPage) {
    setBusy(true);
    try {
      const recovered = await api.recoverDeletedPage(session, workspaceId, page.pageId);
      if (page.row) {
        await restoreDatabaseRowPage(session, page.pageId, { requestId: newId(), expectedAuthorityRevision: recovered.authorityRevision });
      } else {
        const trashed = await api.readPage(session, page.pageId);
        await sendKeptPageMetadata(session, {
          baseline: pageMetadataBaseline(trashed), change: { inTrash: false }, mutationId: newId(), pageId: page.pageId,
        });
      }
      leave(page.pageId);
      notify(t("deletedPagesRecovered"), "success");
    } catch {
      notify(t("deletedPagesCouldntRecover"), "error");
    } finally {
      setBusy(false);
    }
  }

  async function purge(page: DeletedPage) {
    const confirmed = await confirmAction({
      message: t("deletedPagesPurgeConfirm"), confirmLabel: t("deletedPagesPurge"), danger: true,
    });
    if (!confirmed) return;
    setBusy(true);
    try {
      await api.purgeDeletedPage(session, workspaceId, page.pageId, { requestId: newId() });
      leave(page.pageId);
      notify(t("deletedPagesPurged"), "success");
    } catch (error) {
      notify(t(isLegalHoldPermanentDeleteError(error) ? "deletedPagesLegalHold" : "deletedPagesCouldntPurge"), "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={styles.section} data-settings-part="deleted-pages">
      {pages === null || (readFailed && pages.length === 0) ? null : pages.length === 0 ? (
        <p className={styles.empty}>{t("deletedPagesEmpty")}</p>
      ) : (
        <ul className={styles.list}>
          {pages.map((page) => (
            <li key={page.pageId} className={styles.row} data-deleted-page={page.pageId}>
              <div className={styles.words}>
                <span className={styles.name}>{titleWithClosedMentions(page.title, page.titleSpans) || t("deletedPagesUntitled")}</span>
                <span className={styles.detail}>
                  {page.deletedByName
                    ? t("deletedPagesDeletedBy", { name: page.deletedByName, date: formatDay(page.deletedAt, i18n.language) })
                    : t("deletedPagesDeletedByTrash", { date: formatDay(page.deletedAt, i18n.language) })}
                  {" · "}
                  {page.legalHold ? t("deletedPagesKeptByHold") : t("deletedPagesGoesOn", { date: formatDay(page.purgeAt, i18n.language) })}
                </span>
              </div>
              <div className={styles.actions}>
                <button type="button" disabled={busy} onClick={() => void recover(page)}>{t("deletedPagesRecover")}</button>
                <button type="button" disabled={busy || page.legalHold} data-danger onClick={() => void purge(page)}>{t("deletedPagesPurge")}</button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {readFailed ? (
        <ListNotice role="alert" onRetry={() => { setReadFailed(false); setReadTurn((turn) => turn + 1); }} data-deleted-pages-read-failed>
          {t("deletedPagesLoadFailed")}
        </ListNotice>
      ) : null}
    </section>
  );
}
