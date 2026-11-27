import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { request } from "@/apiTransport";
import {
  appPlatformOperations, type AppRunChangesWindow, type AppRunComment, type AppRunHistory, type AppRunHistoryList, type AppRunPage, type UsableApp,
} from "@/appPlatformWire.generated";
import { newId } from "@/lib/ids";
import { appText } from "@/components/admin/administratorAppsApi";
import { pageHistoryRevertStatus } from "@/components/PageHistoryRevertButtons";
import { activeDateLocale } from "@/lib/i18n";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { listPageHistoryVersions, revertPageHistoryChanges } from "@/lib/pageHistoryService";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import dialogStyles from "../admin/AdministratorApps.module.css";
import { runAppMenuAction } from "./appMenuAction";
import styles from "./AppRunHistoryDialog.module.css";
import { listedPageNameKind, untitledPageDisplayTitle } from "../../lib/pageTitle";

/**
 * What the app did for the person here (docs/app-platform.md 10): its
 * recent actions — from its screen, an agent or an automation — with the
 * pages each changed in Leafen, to undo page by page (the app's changes
 * from then on; what others changed stays), an undo action when the app
 * has one, and what reached outside Leafen marked as not undone here. The
 * runs, and a run's pages and comments, go on with "더 보기".
 */
export function AppRunHistoryDialog({ session, usable, name, onClose }: {
  session: Session;
  usable: Pick<UsableApp, "installId" | "tenancyKey" | "name" | "slug">;
  name: string;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["common", "updatesPanel"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [runs, setRuns] = useState<AppRunHistory[] | null>(null);
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState("");
  const [done, setDone] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  // The runs from the cursor on, or a run's next pages and comments.
  const runsPath = (cursor: string) => `${appPlatformOperations.runHistory.path.replace("{installId}", encodeURIComponent(usable.installId))}?${
    new URLSearchParams({ tenancy: usable.tenancyKey, ...(cursor ? { cursor } : {}) })}`;
  useEffect(() => {
    let live = true;
    request<AppRunHistoryList>(runsPath(""), { method: appPlatformOperations.runHistory.method }, session).then(
      (listed) => { if (live) { setRuns(listed.runs); setNext(listed.nextCursor ?? ""); } },
      (failure) => { if (live) setError(settingsErrorMessage(failure, t("appHost.did.failed"))); },
    );
    return () => { live = false; };
    // The path follows the install and tenancy listed here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, t, usable.installId, usable.tenancyKey]);

  async function more(run?: AppRunHistory) {
    setBusy(run ? `${run.id}/more` : "more");
    setError("");
    try {
      if (!run) {
        const listed = await request<AppRunHistoryList>(runsPath(next), { method: appPlatformOperations.runHistory.method }, session);
        setRuns((current) => [...(current ?? []), ...listed.runs]);
        setNext(listed.nextCursor ?? "");
      } else {
        // A window can come back empty — the server looks at only so many of
        // the run's pages, and leaves out rows named under their database —
        // so it reads on until it has something or the last is read.
        let cursor = run.moreCursor ?? "";
        const pages: AppRunPage[] = [];
        const comments: AppRunComment[] = [];
        do {
          const path = `${appPlatformOperations.runChanges.path.replace("{installId}", encodeURIComponent(usable.installId))
            .replace("{runId}", encodeURIComponent(run.id))}?${new URLSearchParams({ tenancy: usable.tenancyKey, cursor })}`;
          const changes: AppRunChangesWindow = await request<AppRunChangesWindow>(path, { method: appPlatformOperations.runChanges.method }, session);
          pages.push(...changes.pages);
          comments.push(...changes.comments);
          cursor = changes.nextCursor ?? "";
        } while (cursor && !pages.length && !comments.length);
        setRuns((current) => current?.map((item) => item.id === run.id ? {
          ...item, pages: [...item.pages, ...pages], comments: [...item.comments, ...comments], moreCursor: cursor,
        } : item) ?? current);
      }
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appHost.did.failed")));
    } finally {
      setBusy("");
    }
  }

  async function revert(run: AppRunHistory, pageId: string, versionId: string, through: number) {
    const key = `${run.id}/${pageId}`;
    setBusy(key);
    setError("");
    try {
      const history = await listPageHistoryVersions(session, pageId);
      const result = await revertPageHistoryChanges(session, pageId, versionId, { baseRevision: history.snapshotFrontier, connection: true, through });
      setDone((current) => ({ ...current, [key]: pageHistoryRevertStatus(t, result, name, "app") }));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appHost.did.revertFailed")));
    } finally {
      setBusy("");
    }
  }

  async function deleteComment(run: AppRunHistory, comment: AppRunComment) {
    const key = `${run.id}/comment/${comment.commentId}`;
    setBusy(key);
    setError("");
    try {
      await api.deleteComment(session, comment.pageId, comment.commentId, { requestId: newId(), expectedRevision: comment.revision });
      setDone((current) => ({ ...current, [key]: t("appHost.did.commentDeleted") }));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appHost.did.commentDeleteFailed")));
    } finally {
      setBusy("");
    }
  }

  async function undo(run: AppRunHistory) {
    if (!run.undoAction) return;
    setBusy(run.id);
    setError("");
    const ok = await runAppMenuAction({
      session, usable: usable as UsableApp, action: run.undoAction, label: t("appHost.did.undoLabel"),
      input: { run: run.id, input: run.undoInput ?? {} }, t, language: i18n.language,
      notice: (message, tone) => { if (tone === "error") setError(message); },
    });
    if (ok) setDone((current) => ({ ...current, [run.id]: t("appHost.did.undone") }));
    setBusy("");
  }

  const when = (at: number) => new Date(at).toLocaleString(activeDateLocale(), { dateStyle: "long", timeStyle: "short" });
  // Where it ran: the menu the person chose it from, else what started it.
  const via = (run: AppRunHistory) => run.place
    ? t(`appHost.did.place.${run.place}`, { defaultValue: run.place })
    : t(`appHost.did.via.${run.trigger}`, { defaultValue: run.trigger });
  // A database's changed rows by their names; another page by its title.
  const changed = (page: AppRunPage) => {
    if (!page.rows.length) return page.title || untitledPageDisplayTitle(listedPageNameKind(page));
    const named = page.rows.slice(0, 3).map((row) => {
      const title = row.title || untitledPageDisplayTitle("page");
      return row.database ? `${row.database} › ${title}` : title;
    });
    // Every row it changed is counted, past the few named.
    const count = page.rowCount ?? page.rows.length;
    return count > 3 ? `${named.join(", ")} ${t("appHost.did.more", { count: count - 3 })}` : named.join(", ");
  };
  return createPortal(
    <div ref={overlayRef} className={dialogStyles.overlay} style={{ zIndex }} data-app-run-history>
      <button type="button" className={dialogStyles.backdrop} tabIndex={-1} aria-label={t("actions.close")} disabled={Boolean(busy)} onClick={onClose} />
      <section ref={dialogRef} className={dialogStyles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={Boolean(busy) || undefined}>
        <h2 id={titleId} className={dialogStyles.dialogTitle}>{t("appHost.did.title", { name })}</h2>
        {runs?.length ? <p className={dialogStyles.note}>{t("appHost.did.body")}</p> : null}
        {runs ? (
          runs.length ? (
            <ul className={styles.runs} data-app-runs-list>
              {runs.map((run) => (
                <li key={run.id} data-app-run={run.id} data-status={run.status}>
                  <span className={styles.head}>
                    <strong>{appText(run.actionName, i18n.language) || run.action}</strong>
                    <span>{[when(run.created), via(run), ...(run.status === "ok" ? [] : [t(`appHost.did.status.${run.status}`, { defaultValue: run.status })])].join(" · ")}</span>
                  </span>
                  {run.pages.map((page) => {
                    const key = `${run.id}/${page.pageId}`;
                    return (
                      <span key={page.pageId} className={styles.page} data-app-run-page={page.pageId}>
                        <span className={styles.pageTitle} title={page.rows.length ? page.title : undefined}>{changed(page)}</span>
                        {done[key] ? <span className={styles.done} role="status">{done[key]}</span>
                          // Taken back by this person before: said so, not offered again (2026-11-06).
                          : page.reverted ? <span className={styles.done} data-app-run-reverted>{t("appHost.did.reverted")}</span> : (
                          <button type="button" className={dialogStyles.secondary} disabled={Boolean(busy)} data-app-run-revert
                            onClick={() => void revert(run, page.pageId, page.versionId, page.through)}>{t("appHost.did.revert")}</button>
                        )}
                      </span>
                    );
                  })}
                  {run.comments.map((comment) => {
                    const key = `${run.id}/comment/${comment.commentId}`;
                    // A comment sits on a page, never on a database's own page.
                    return (
                      <span key={comment.commentId} className={styles.page} data-app-run-comment={comment.commentId}>
                        <span className={styles.pageTitle}>{t(comment.text ? "appHost.did.comment" : "appHost.did.commentNoText", {
                          page: comment.pageTitle || untitledPageDisplayTitle("page"), text: comment.text })}</span>
                        {done[key] ? <span className={styles.done} role="status">{done[key]}</span> : comment.deletable ? (
                          <button type="button" className={dialogStyles.secondary} disabled={Boolean(busy)} data-app-run-comment-delete
                            onClick={() => void deleteComment(run, comment)}>{t("appHost.did.commentDelete")}</button>
                        ) : null}
                      </span>
                    );
                  })}
                  {run.moreCursor ? (
                    <span className={styles.page}>
                      <button type="button" className={dialogStyles.secondary} disabled={Boolean(busy)} data-app-run-more
                        onClick={() => void more(run)}>{t("appHost.more")}</button>
                    </span>
                  ) : null}
                  {run.undoAction ? (
                    <span className={styles.page}>
                      <span className={styles.pageTitle}>{t("appHost.did.undoBody")}</span>
                      {done[run.id] ? <span className={styles.done} role="status">{done[run.id]}</span> : (
                        <button type="button" className={dialogStyles.secondary} disabled={Boolean(busy)} data-app-run-undo
                          onClick={() => void undo(run)}>{t("appHost.did.undo")}</button>
                      )}
                    </span>
                  ) : null}
                  {run.externalEffect ? <span className={styles.outside} data-app-run-outside>{t("appHost.did.outside")}</span> : null}
                  {!run.pages.length && !run.comments.length && run.writes > 0
                    ? <span className={styles.none} data-app-run-unlisted>{t("appHost.did.unlisted")}</span> : null}
                  {!run.pages.length && !run.comments.length && !run.writes && !run.undoAction && !run.externalEffect
                    ? <span className={styles.none}>{t("appHost.did.nothing")}</span> : null}
                </li>
              ))}
            </ul>
          ) : <p className={dialogStyles.note} data-app-runs-none>{t("appHost.did.none")}</p>
        ) : null}
        {next ? (
          <button type="button" className={`${dialogStyles.secondary} ${dialogStyles.listMore}`} disabled={Boolean(busy)} data-app-runs-more
            onClick={() => void more()}>{t("appHost.more")}</button>
        ) : null}
        {error ? <div className={dialogStyles.error} role="alert">{error}</div> : null}
        <div className={dialogStyles.dialogActions}>
          <button ref={closeRef} type="button" className={dialogStyles.secondary} disabled={Boolean(busy)} onClick={onClose}>{t("actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
