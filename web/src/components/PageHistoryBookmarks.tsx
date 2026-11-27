"use client";

import { type FormEvent, useCallback, useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { isWriteUncertainText, userErrorText } from "@/lib/settingsErrors";
import { useWaitingNote } from "@/lib/pendingChoice";
import { useReadAgainAfterUncertainWrites } from "@/lib/uncertainWrites";
import { BookmarkIcon, DotsHorizontal, PencilSmallIcon, Trash } from "@/icons/leafen";
import {
  createPageHistoryBookmark,
  deletePageHistoryBookmark,
  listPageHistoryBookmarks,
  renamePageHistoryBookmark,
} from "@/lib/pageHistoryService";
import { pageHistoryBounds, type PageHistoryBookmark } from "@/pageHistoryWire.generated";
import type { Session } from "@/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import styles from "./PageHistoryBookmarks.module.css";

// A list entry's action, named in its tip above it, clear of the time under
// it, as Notion's history list does (measured 2026-11-08).
const rowAction = { className: styles.rowAction, "data-tip-side": "top" } as const;

const newestFirst = (items: PageHistoryBookmark[]) =>
  [...items].sort((left, right) => right.versionCreatedAt - left.versionCreatedAt || right.createdAt - left.createdAt);

/**
 * The page's bookmarked versions, as Notion's version history keeps them
 * (seen 2026-10-01): each a named mark on one version, newest version first.
 * A change that fails throws for the history to say so.
 */
export function usePageHistoryBookmarks(session: Session, pageId: string, onError: (message: string) => void) {
  const [bookmarks, setBookmarks] = useState<PageHistoryBookmark[]>([]);
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  // A bookmark made or removed without the server's answer may have been
  // taken after all: once the server answers again, the list is read again.
  const readAgain = useReadAgainAfterUncertainWrites();
  // Read once a page and signed-in session: a new copy of the same session
  // reads nothing again; each read takes the latest copy.
  const sessionKey = session.sessionId ?? `${session.user.id}:${session.token}`;
  const latestSession = useRef(session);
  latestSession.current = session;
  useEffect(() => {
    const controller = new AbortController();
    listPageHistoryBookmarks(latestSession.current, pageId, controller.signal).then((items) => {
      if (!controller.signal.aborted) setBookmarks(newestFirst(items));
    }).catch((caught) => {
      if (!controller.signal.aborted) onErrorRef.current(userErrorText(caught, i18next.t("updatesPanel:versions.loadFailed")));
    });
    return () => controller.abort();
  }, [pageId, sessionKey, readAgain]);

  const create = useCallback(async (versionId: string, name: string) => {
    const made = await createPageHistoryBookmark(session, pageId, versionId, name);
    setBookmarks((current) => newestFirst([...current.filter((item) => item.id !== made.id), made]));
    return made;
  }, [pageId, session]);
  const rename = useCallback(async (bookmark: PageHistoryBookmark, name: string) => {
    const renamed = await renamePageHistoryBookmark(session, bookmark, name);
    setBookmarks((current) => current.map((item) => (item.id === renamed.id ? renamed : item)));
  }, [session]);
  const remove = useCallback(async (bookmark: PageHistoryBookmark) => {
    await deletePageHistoryBookmark(session, bookmark);
    setBookmarks((current) => current.filter((item) => item.id !== bookmark.id));
  }, [session]);
  return { bookmarks, create, rename, remove };
}

/**
 * Naming a version as it is bookmarked, or renaming its bookmark, as
 * Notion's 480px dialog: the version's whole time is the field's
 * placeholder and names a bookmark left unnamed.
 */
export function PageHistoryBookmarkNameDialog({
  mode, time, initialName = "", onSubmit, onClose,
}: {
  mode: "create" | "rename";
  time: string;
  initialName?: string;
  onSubmit: (name: string) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const titleId = useId();
  const [name, setName] = useState(initialName);
  const [busy, setBusy] = useState(false);
  // What the save says under the field: the server not answering after ten
  // seconds, then why it wasn't saved or that it isn't known whether it was
  // (2026-11-06: the words went to the history's top bar, in red, while
  // this stayed open with nothing said).
  const [failure, setFailure] = useState("");
  const waitingNote = useWaitingNote(busy);
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLFormElement>(null);
  const layer = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, onEscape: onClose,
    onOutsidePress: onClose, modalOwned: () => [dialogRef.current?.parentElement] });
  useEffect(() => {
    // The caret after the name, as Notion's rename leaves it (seen 2026-11-06;
    // measured 2026-11-16 in Chrome at 1440 with no script before the press:
    // "북마크 이름 바꾸기" on "abc", "z" typed made "abcz").
    const input = inputRef.current;
    input?.focus();
    input?.setSelectionRange(input.value.length, input.value.length);
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setFailure("");
    try {
      await onSubmit(name.trim());
    } catch (caught) {
      setFailure(userErrorText(caught, i18next.t("updatesPanel:versions.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.scrim} style={{ zIndex: layer.zIndex }}>
      <form ref={dialogRef} className={styles.nameDialog} role="dialog" aria-modal="true" aria-labelledby={titleId}
        data-page-history-bookmark-dialog={mode} onSubmit={(event) => void submit(event)}
        onKeyDown={layer.onKeyDown}>
        <span className={styles.nameMark} aria-hidden="true"><BookmarkIcon size="regular" /></span>
        <h2 id={titleId} className={styles.nameTitle}>
          {t(mode === "create" ? "updatesPanel:versions.nameVersionTitle" : "updatesPanel:versions.renameVersionTitle")}
        </h2>
        <p className={styles.nameBody}>
          {t(mode === "create" ? "updatesPanel:versions.nameVersionBody" : "updatesPanel:versions.renameVersionBody", { time })}
        </p>
        <input ref={inputRef} className={styles.nameInput} value={name} placeholder={time}
          maxLength={pageHistoryBounds.bookmarkNameMaxChars} aria-labelledby={titleId}
          onChange={(event) => setName(event.target.value)} />
        {failure || waitingNote ? <p className={failure && !isWriteUncertainText(failure) ? styles.nameFailure : styles.nameWaiting}
          role={failure ? "alert" : "status"} data-page-history-bookmark-note>{failure || waitingNote}</p> : null}
        <div className={styles.nameActions}>
          <button type="button" className={styles.quiet} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="submit" className={styles.primary} disabled={busy}>
            {t(mode === "create" ? "updatesPanel:versions.bookmarkDone" : "updatesPanel:versions.bookmarkSave")}
          </button>
        </div>
      </form>
    </div>
  );
}

/** Asks before taking a bookmark away, as Notion's 252px dialog: its version stays. */
export function PageHistoryBookmarkDeleteDialog({ onDelete, onClose }: {
  onDelete: () => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const titleId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const layer = useOverlayLayer(true, { kind: "alertdialog", ref: dialogRef, onEscape: onClose,
    onOutsidePress: onClose, modalOwned: () => [dialogRef.current?.parentElement] });
  const [busy, setBusy] = useState(false);
  // "취소" takes the focus, unringed until a key, so Enter keeps the
  // bookmark: Notion's, measured 2026-11-16 twice in two Chrome tabs at 1440
  // with no script before the press — "북마크 삭제" pressed, no ring, "z"
  // typed rang "취소", the focus there.
  useEffect(() => cancelRef.current?.focus(), []);
  return (
    <div className={styles.scrim} style={{ zIndex: layer.zIndex }}>
      <div ref={dialogRef} className={styles.deleteDialog} role="alertdialog" aria-modal="true" aria-labelledby={titleId}
        data-page-history-bookmark-delete onKeyDown={layer.onKeyDown}>
        <span className={styles.deleteMark} aria-hidden="true"><Trash size="large" /></span>
        <h2 id={titleId} className={styles.deleteTitle}>{t("updatesPanel:versions.deleteBookmarkTitle")}</h2>
        <p className={styles.deleteBody}>{t("updatesPanel:versions.deleteBookmarkBody")}</p>
        <div className={styles.deleteActions}>
          <button ref={cancelRef} type="button" className={styles.secondary} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="button" className={styles.danger} disabled={busy} onClick={() => {
            setBusy(true);
            void onDelete().finally(() => setBusy(false));
          }}>{t("common:actions.delete")}</button>
        </div>
      </div>
    </div>
  );
}

/**
 * A bookmark in the history's list, as Notion's: a red bookmark and its name
 * over the version's time to the second, and its options (rename, delete).
 */
export function PageHistoryBookmarkRow({
  bookmark, name, time, selected, canEdit, onSelect, onRename, onDelete,
}: {
  bookmark: PageHistoryBookmark;
  name: string;
  time: string;
  selected: boolean;
  canEdit: boolean;
  onSelect: () => void;
  onRename: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation("updatesPanel");
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const layer = useOverlayLayer(menuOpen, { kind: "menu", ref: menuRef, inside: () => [buttonRef.current],
    onEscape: () => setMenuOpen(false), onOutsidePress: () => setMenuOpen(false) });
  // Rename's mark is Notion's 16px small pencil and delete's its 20px trash,
  // each in a 20px place (measured 2026-11-15).
  const actions = [{ id: "rename", label: t("versions.renameBookmark"), icon: <PencilSmallIcon size="small" aria-hidden="true" />, choose: onRename },
    { id: "delete", label: t("versions.deleteBookmark"), icon: <Trash size="regular" aria-hidden="true" />, choose: onDelete }];
  const picker = useNativeMenu({ items: actions, open: menuOpen, menuRef, label: t("versions.bookmarkOptions"),
    keyOf: item => item.id, textOf: item => item.label, onAction: item => { setMenuOpen(false); item.choose(); } });

  return (
    // A bookmark is an entry of the list as a version is, 1px apart over its
    // line, as Notion's (seen 2026-11-13 and 2026-11-15).
    <div className={styles.bookmarkItem} role="presentation" data-page-history-bookmark={bookmark.id} data-version-entry="">
      <button type="button" role="option" aria-selected={selected} className={styles.bookmarkRow}
        data-selected={selected ? "true" : undefined} onClick={onSelect}>
        <span className={styles.bookmarkName}>
          <span className={styles.bookmarkGlyph} aria-hidden="true"><BookmarkIcon size="small" /></span>
          <span className={styles.bookmarkLabel}>{name}</span>
        </span>
        <span className={styles.bookmarkTime}>{time}</span>
      </button>
      {canEdit ? (
        <button ref={buttonRef} type="button" {...rowAction} aria-label={t("versions.bookmarkOptions")}
          title={t("versions.bookmarkOptions")} aria-haspopup="menu" aria-expanded={menuOpen}
          data-page-history-bookmark-options onClick={() => setMenuOpen((open) => !open)}>
          <DotsHorizontal size="tiny" aria-hidden="true" />
        </button>
      ) : null}
      {menuOpen ? (
        <div {...picker.menuProps} ref={menuRef} className={styles.optionsMenu} style={{ zIndex: layer.zIndex }}
          onKeyDown={event => { picker.menuProps.onKeyDown?.(event); layer.onKeyDown(event); }}>
          {actions.map(item => <NativeMenuItem key={item.id} state={picker.state} itemKey={item.id} className={styles.optionsItem}>
            {({ labelProps }) => <><span className={styles.optionsGlyph}>{item.icon}</span><span {...labelProps}>{item.label}</span></>}
          </NativeMenuItem>)}
        </div>
      ) : null}
    </div>
  );
}

/**
 * A version entry's "이 버전 북마크하기", over the entry's right end, or
 * beside its "버전의 모든 변경 사항 보기" there.
 */
export function PageHistoryBookmarkVersionButton({ onBookmark, beside = false }: { onBookmark: () => void; beside?: boolean }) {
  const { t } = useTranslation("updatesPanel");
  return (
    <button type="button" {...rowAction} aria-label={t("versions.bookmarkVersion")}
      data-beside={beside ? "true" : undefined}
      title={t("versions.bookmarkVersion")} data-page-history-bookmark-version onClick={onBookmark}>
      <BookmarkIcon size="tiny" aria-hidden="true" />
    </button>
  );
}

/** A bookmarked version's mark where its bookmark button was: filled, as Notion's (seen 2026-11-06). */
export function PageHistoryBookmarkedMark() {
  const { t } = useTranslation("updatesPanel");
  return (
    <span {...rowAction} data-beside="true" data-bookmarked="true" role="img"
      aria-label={t("versions.versionBookmarked")} title={t("versions.versionBookmarked")}>
      <BookmarkIcon size="tiny" aria-hidden="true" />
    </span>
  );
}

export const pageHistoryBookmarkItemClass = styles.bookmarkItem;
export const pageHistoryRowAction = rowAction;
