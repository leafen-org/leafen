import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AdministratorWorkspaceFile, AdministratorWorkspaceStoragePerson } from "@/administratorWire.generated";
import { administratorWorkspaceContent } from "@/administratorWorkspaceContentApi";
import { saveStreamedResponse } from "@/lib/nativeArchiveDownload";
import { userErrorText } from "@/lib/settingsErrors";
import { ReadFailed } from "../ReadFailed";
import type { Session } from "../../types";
import { confirmAction } from "../ConfirmActionDialog";
import { formatFileSize } from "../database/files";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorWorkspaceContent.module.css";
import { displayDate } from "@/lib/displayDate";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";

/**
 * A workspace's files for a server administrator: the largest first, or what each person's
 * take; each to download, and one no page uses to delete. Both lists go on with "더 보기".
 */
export function AdministratorWorkspaceFiles({ session, workspaceId, onChanged }: {
  session: Session; workspaceId: string;
  /** A file was deleted: the workspace's counts and size changed. */
  onChanged?: () => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [view, setView] = useState<"files" | "people">("files");
  const [files, setFiles] = useState<AdministratorWorkspaceFile[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  const [people, setPeople] = useState<AdministratorWorkspaceStoragePerson[]>();
  const [peopleCursor, setPeopleCursor] = useState<string | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // A read that failed, said where its list shows with a way to read it again.
  const [readFailed, setReadFailed] = useState<{ text: string; again: () => void } | null>(null);

  async function loadFiles(next?: string) {
    setReadFailed(null);
    try {
      const page = await administratorWorkspaceContent.files(session, workspaceId, next);
      setFiles((current) => next ? [...(current ?? []), ...page.items] : page.items);
      setCursor(page.nextCursor);
    } catch (cause) {
      setReadFailed({ text: userErrorText(cause, t("workspaceContent.loadFailed")), again: () => void loadFiles(next) });
    }
  }

  useEffect(() => {
    void loadFiles();
    // The files load once for the workspace shown.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, workspaceId]);

  async function loadPeople(next?: string) {
    setReadFailed(null);
    try {
      const page = await administratorWorkspaceContent.storage(session, workspaceId, next);
      setPeople((current) => next ? [...(current ?? []), ...page.items] : page.items);
      setPeopleCursor(page.nextCursor);
    } catch (cause) {
      setReadFailed({ text: userErrorText(cause, t("workspaceContent.loadFailed")), again: () => void loadPeople(next) });
    }
  }

  useEffect(() => {
    if (view !== "people" || people) return;
    void loadPeople();
    // People load the first time they are shown.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view]);

  async function download(file: AdministratorWorkspaceFile) {
    setBusy(file.id);
    setError("");
    try {
      const response = await administratorWorkspaceContent.downloadFile(session, workspaceId, file.id);
      // The save picker wants a plain type and one extension at least.
      await saveStreamedResponse(response, file.name, {
        description: file.name,
        mimeType: file.contentType.split(";")[0].trim() || "application/octet-stream",
        extensions: [/\.[A-Za-z0-9]{1,10}$/u.exec(file.name)?.[0] ?? ".bin"],
      });
    } catch (cause) {
      setError(userErrorText(cause, t("workspaceContent.failed")));
    } finally {
      setBusy("");
    }
  }

  async function remove(file: AdministratorWorkspaceFile) {
    const confirmed = await confirmAction({
      message: t("workspaceContent.files.deleteMessage", { name: file.name }),
      detail: t(file.historyOnly ? "workspaceContent.files.deleteHistoryOnlyDetail" : "workspaceContent.files.deleteDetail"),
      confirmLabel: t("workspaceContent.files.delete"),
      danger: true,
    });
    if (!confirmed) return;
    setBusy(file.id);
    setError("");
    try {
      await administratorWorkspaceContent.deleteFile(session, workspaceId, file.id);
      setFiles((current) => current?.filter((item) => item.id !== file.id));
      setPeople(undefined);
      onChanged?.();
    } catch (cause) {
      setError(userErrorText(cause, t("workspaceContent.failed")));
    } finally {
      setBusy("");
    }
  }

  const date = (value: string) => value ? displayDate(value) : "";
  return (
    <div>
      <div className={own.switch} role="group">
        <button type="button" aria-pressed={view === "files"} onClick={() => setView("files")}>{t("workspaceContent.files.files")}</button>
        <button type="button" aria-pressed={view === "people"} onClick={() => setView("people")}>{t("workspaceContent.files.people")}</button>
      </div>
      {view === "files" ? (
        <div className={own.list}>
          {(files ?? []).map((file) => (
            <div key={file.id} className={own.item} data-admin-file={file.id}>
              <span className={own.itemText}>
                {/* Its size stays whole however long its name (own.itemName). */}
                <strong className={own.itemName}><span>{file.name}</span><span>{` · ${formatFileSize(file.size)}`}</span></strong>
                {/* Its page by name, an untitled one by what its kind is called (untitledPageDisplayTitle). */}
                <span>{[file.createdByName, file.pageId ? titleWithClosedMentions(file.pageTitle, file.pageTitleSpans)
                  || untitledPageDisplayTitle(listedPageNameKind(file)) : "", date(file.createdAt)].filter(Boolean).join(" · ")}</span>
              </span>
              <span className={own.itemActions}>
                <button type="button" className={styles.secondaryButton} disabled={busy === file.id} onClick={() => void download(file)}>
                  {t("workspaceContent.files.download")}
                </button>
                {file.held ? <span className={own.tag}>{t("workspaceContent.files.held")}</span>
                  : file.referenced ? <span className={own.tag} title={t("workspaceContent.files.inUseHint")}>{t("workspaceContent.files.inUse")}</span>
                  : (<>
                    {/* Kept only by pages' version history: it may go, and says so. */}
                    {file.historyOnly ? <span className={own.tag} title={t("workspaceContent.files.historyOnlyHint")}>{t("workspaceContent.files.historyOnly")}</span> : null}
                    <button type="button" className={styles.secondaryButton} disabled={busy === file.id} onClick={() => void remove(file)}>
                      {t("workspaceContent.files.delete")}
                    </button>
                  </>)}
              </span>
            </div>
          ))}
          {files && !files.length ? <div className={own.hint}>{t("workspaceContent.files.empty")}</div> : null}
          {cursor ? <button type="button" className={styles.secondaryButton} onClick={() => void loadFiles(cursor)}>{t("workspaceContent.more")}</button> : null}
        </div>
      ) : (
        <div className={own.list}>
          {(people ?? []).map((person) => (
            <div key={person.userId} className={own.item}>
              <span className={own.itemText}>
                <strong>{person.name || person.email || t("workspaceContent.unknownPerson")}</strong>
                <span>{[person.email, t("workspaceContent.files.personFiles", { count: person.fileCount })].filter(Boolean).join(" · ")}</span>
              </span>
              <span className={own.tag}>{formatFileSize(person.storageBytes)}</span>
            </div>
          ))}
          {people && !people.length ? <div className={own.hint}>{t("workspaceContent.files.empty")}</div> : null}
          {peopleCursor ? (
            <button type="button" className={styles.secondaryButton} data-admin-storage-more
              onClick={() => void loadPeople(peopleCursor)}>{t("workspaceContent.more")}</button>
          ) : null}
        </div>
      )}
      {readFailed ? <ReadFailed message={readFailed.text} onRetry={readFailed.again} /> : null}
      {error ? <div className={own.error} role="alert">{error}</div> : null}
    </div>
  );
}
