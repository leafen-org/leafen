import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { UsableApp, UsableAppFileOpener } from "@/appPlatformWire.generated";
import { request } from "@/apiTransport";
import { appText } from "@/components/admin/administratorAppsApi";
import { Download, X } from "@/icons/leafen";
import { api } from "@/api";
import { triggerBrowserFileDownload, triggerBrowserURLDownload } from "@/lib/browserFileDownload";
import { createFileDownloadCapabilityResolver } from "@/lib/fileDownloadCapability";
import { AddFileToPageButton } from "./DocumentPreview";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { touchScreen } from "@/lib/screenKind";
import { followVisualViewport } from "@/lib/visualViewportPlace";
import type { Session } from "@/types";
import type { AppFileConflict, AppFileConflictChoice, AppFileEdits, AppFileSaveTarget } from "./appFileSave";
import { AppPlaceFrame } from "./AppPlaceFrame";
import { announceAppFileSaved } from "./appFileOpening";
import { confirmAction } from "../ConfirmActionDialog";
import styles from "./AppFileOpener.module.css";
import { appBridgeRefusal } from "./appFrameBridge";
import { useNoteUnderHead } from "./useNoteUnderHead";

// A file opened with an app from its block's menu (document apps decision of
// 2026-10-10, 5 and 11): the app's screen over the page, as Notion's media
// full screen, closed by its button or Escape. The app learns the file — its
// block, page, id, name, type and size — and whether the person may change
// the page; it reads the file through the native API as the person. Nobody
// holds the file: Leafen shows who else has it open, and when a save meets a
// newer version, asks here, outside the app, how to keep it.

export type AppOpenedFile = {
  workspaceId: string; pageId: string; blockId: string; fileId: string; name: string; contentType: string; size: number;
  canEdit: boolean;
  /** The version opened, as saving names it. */
  version: string;
};

const OPEN_RENEW_MS = 15_000;

function pageAreaLeft() {
  const main = document.querySelector("main");
  return main ? Math.max(0, Math.round(main.getBoundingClientRect().left)) : 0;
}

/**
 * The apps, and their openers, that open a file of this name and type: an
 * app once, as the page's organization has it when the person has it in more
 * than one.
 */
export function fileOpenersFor(apps: readonly UsableApp[], name: string, contentType: string, organizationId?: string) {
  const extension = /\.[^./]+$/u.exec(name.toLowerCase())?.[0] ?? "";
  const type = contentType.split(";")[0].trim().toLowerCase();
  const found = new Map<string, { app: UsableApp; opener: UsableAppFileOpener }>();
  for (const app of apps) {
    for (const opener of app.fileOpeners ?? []) {
      if (!opener.types.some((kind) => kind === extension || kind === type)) continue;
      const key = `${app.installId}:${opener.key}`;
      if (!found.has(key) || app.tenancyKey === `org:${organizationId}`) found.set(key, { app, opener });
    }
  }
  return [...found.values()];
}

export function AppFileOpener({ session, app, opener, file, canEditNow = true, onClose }: {
  session: Session;
  app: UsableApp;
  opener: UsableAppFileOpener;
  file: AppOpenedFile;
  /** Whether the page can still be edited by the person; when it no longer can, saving stops and the window says so. */
  canEditNow?: boolean;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation("common");
  const backdrop = useRef<HTMLDivElement>(null), root = useRef<HTMLDivElement>(null), close = useRef<HTMLButtonElement>(null);
  const head = useRef<HTMLElement>(null), othersLine = useRef<HTMLSpanElement>(null);
  const port = useRef<MessagePort | null>(null);
  // The file in view: the one opened, then a copy this person kept or the
  // latest they reopened.
  const [shown, setShown] = useState(file);
  const [generation, setGeneration] = useState(0);
  const [others, setOthers] = useState<string[]>([]);
  const [conflict, setConflict] = useState<{ found: AppFileConflict; settle: (choice: AppFileConflictChoice) => void } | null>(null);
  const version = useRef(file.version);
  const edits = Boolean(opener.edit) && shown.canEdit && canEditNow;
  const editingEnded = Boolean(opener.edit) && shown.canEdit && !canEditNow;
  // Whether the app has changes not saved yet, as it told (setUnsaved), and
  // whether it tells at all: one that never does saves whatever it sends.
  const unsaved = useRef(false);
  const tells = useRef(false);
  // A fresh frame on the latest file starts with nothing unsaved: the old
  // frame's changes went with it, and the new one only tells when it changes
  // (2026-11-07: after "최신본 열기" closing asked about changes long gone).
  const restart = () => {
    unsaved.current = false;
    setGeneration((count) => count + 1);
  };
  // Changes not saved yet go only once the person says so.
  const mayClose = async () => !unsaved.current || await confirmAction({
    message: t("appPlace.closeUnsaved"), detail: t("appPlace.closeUnsavedDetail"),
    confirmLabel: t("appPlace.closeWithoutSaving"), danger: true,
  });
  const requestClose = async () => { if (await mayClose()) onClose(); };
  // Out of sight while the file goes to a page (AddFileToPageButton), the page behind it free.
  const [leaving, setLeaving] = useState(false);
  // Leaving the page asks too, while there are unsaved changes.
  useEffect(() => {
    const ask = (event: BeforeUnloadEvent) => {
      if (!unsaved.current) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", ask);
    return () => window.removeEventListener("beforeunload", ask);
  }, []);
  // The app gone from the server for this window (removed, not installed
  // again for the person): the window stays with the changes in it, says it
  // can't save, and gives what the app last tried to save as a file.
  const [appGone, setAppGone] = useState(false);
  const [notSaved, setNotSaved] = useState<AppFileEdits | null>(null);
  const downloadNotSaved = () => {
    if (notSaved) void triggerBrowserFileDownload(new Blob([notSaved.bytes], { type: notSaved.contentType }), notSaved.name, { revokeAfterMs: 60_000 });
  };
  const extra = async (method: string, params: Record<string, unknown>) => {
    if (method !== "setUnsaved") throw appBridgeRefusal("unknownCall");
    tells.current = true;
    unsaved.current = params.unsaved === true;
    return null;
  };
  const fileSave = useRef<AppFileSaveTarget | null>(null);
  fileSave.current = edits ? {
    workspaceId: shown.workspaceId, pageId: shown.pageId, blockId: shown.blockId, name: shown.name, contentType: shown.contentType,
    get baseVersion() { return version.current; },
    set baseVersion(next: string) { version.current = next; },
    get nothingToSave() { return tells.current && !unsaved.current; },
    copyName: copyNameOf(shown.name, (base) => t("appPlace.copyName", { name: base })),
    // When that name is taken on the page, the server numbers it: "{n}" is 2, 3…
    copyNameNumbered: copyNameOf(shown.name, (base) => t("appPlace.copyNameNumbered", { name: base, n: "{n}" })),
    choose: (found) => new Promise((settle) => setConflict({ found, settle })),
    kept: (copy) => {
      version.current = copy.version;
      setShown((before) => ({ ...before, blockId: copy.blockId, fileId: copy.fileId, name: copy.name, size: copy.size, version: copy.version }));
    },
    renamed: (name) => setShown((before) => ({ ...before, name })),
    // The saved file is the one in view, as a frame opened again reads it and the head's download
    // gives it (2026-11-20: after a save the window's frame, opened again on the app installed
    // again, read the file as first opened). A save that went through ends the notice too.
    saved: (file) => {
      setShown((before) => ({ ...before, fileId: file.fileId, size: file.size, version: version.current }));
      setNotSaved(null);
      setAppGone(false);
      announceAppFileSaved(shown.pageId);
    },
    reopen: (latest) => {
      version.current = latest.version;
      setShown((before) => ({ ...before, fileId: latest.fileId, name: latest.name, size: latest.size, version: latest.version }));
      restart();
    },
    notSaved: setNotSaved,
    appGone: () => setAppGone(true),
  } : null;

  // Saying the file is open, while it is, answers who else has it open and,
  // the first time, what the block holds now: the page can be behind the
  // server, and a file saved a moment ago opened as it was before.
  const checkedLatest = useRef(false);
  useEffect(() => {
    let live = true;
    const path = `/pages/${encodeURIComponent(shown.pageId)}/blocks/${encodeURIComponent(shown.blockId)}/file-open`;
    const say = () => request<{ others: { name: string }[]; current?: AppFileConflict }>(path, { method: "PUT" }, session)
      .then((answer) => {
        if (!live) return;
        setOthers(answer.others.map((other) => other.name));
        const latest = answer.current;
        if (!checkedLatest.current && latest && latest.version !== version.current) {
          version.current = latest.version;
          setShown((before) => ({ ...before, fileId: latest.fileId, name: latest.name, size: latest.size, version: latest.version }));
          restart();
        }
        checkedLatest.current = true;
      })
      .catch(() => undefined);
    void say();
    const timer = window.setInterval(() => void say(), OPEN_RENEW_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
      void request(path, { method: "DELETE" }, session).catch(() => undefined);
    };
  }, [session, shown.pageId, shown.blockId]);

  // The question about a newer version takes the focus as it shows, so keys go to it, not to the
  // app's editor under it (2026-11-19: a finger's Save leaves the editor its focus and iPad Safari
  // its keyboard, and letters went on into the document behind the question). Answered, the app's
  // frame has the focus again and the app gives its editor the caret back, so the next key lands
  // there as after any save (2026-11-19: it went to the window's close button); "최신본 열기" opens
  // the app again in a new frame.
  const conflictBox = useRef<HTMLDivElement>(null);
  useEffect(() => { if (conflict) conflictBox.current?.focus({ preventScroll: true }); }, [conflict]);
  const settle = (choice: AppFileConflictChoice) => {
    conflict?.settle(choice);
    setConflict(null);
    if (choice !== "latest") root.current?.querySelector<HTMLIFrameElement>("iframe[data-app-place-frame]")?.focus({ preventScroll: true });
  };
  // The page area's edge, so the sidebar stays in sight as Notion's preview
  // leaves it; a phone's page area starts at the window's edge.
  const [left, setLeft] = useState(pageAreaLeft);
  useEffect(() => {
    const follow = () => setLeft(pageAreaLeft());
    window.addEventListener("resize", follow);
    return () => window.removeEventListener("resize", follow);
  }, []);
  // A touch screen's soft keyboard covers the window's foot without making
  // it shorter (iPad Safari), and an app's editor in the frame can't see it:
  // the caret went on under the keyboard, or the window slid up and took the
  // head and the app's own Save out of sight (seen 2026-11-14 at 1210×834).
  // The screen keeps to what shows above the keyboard, the frame shortened to
  // it, as a phone's sheets do.
  const [touch] = useState(touchScreen);
  useEffect(() => (touch ? followVisualViewport(backdrop.current) : undefined), [touch]);
  const { zIndex } = useOverlayLayer(!leaving, { kind: "dialog", ref: root, modalOwned: () => [root.current], restoreFocus: true,
    onEscape: () => (conflict ? settle("cancel") : void requestClose()) });
  useEffect(() => { close.current?.focus(); }, []);
  const name = appText(opener.name, i18n.language) || appText(app.name, i18n.language);
  // The file in view as saved last, read through its own download capability.
  const downloadShown = async () => {
    const resolver = createFileDownloadCapabilityResolver(() => api.managedFileCapability(session, shown.fileId));
    try { triggerBrowserURLDownload((await resolver.resolve()).url, shown.name); } catch { /* the row's download stays */ } finally { resolver.clear(); }
  };
  const someone = (who: string | undefined) => who || t("appPlace.someone");
  // Who else has it open is read whole: under the head when the head can't hold it beside its buttons.
  const othersText = !others.length ? "" : others.length === 1 ? t("appPlace.fileAlsoOpen", { name: someone(others[0]) })
    : t("appPlace.fileAlsoOpenMany", { name: someone(others[0]), count: others.length - 1 });
  const othersUnder = useNoteUnderHead(head, othersLine, othersText);
  return createPortal(
    <div ref={backdrop} className={styles.backdrop} style={{ zIndex, left, visibility: leaving ? "hidden" : undefined }} data-visual-viewport={touch ? "" : undefined}>
      <div ref={root} className={styles.dialog} role="dialog" aria-modal="true" aria-label={`${shown.name} — ${name}`} data-app-file-opener={opener.key}>
        <header ref={head} className={styles.header}>
          {/* As Notion's file preview, the head names nothing (2026-11-06); the window's label does. */}
          <span className={styles.spacer} />
          {othersText && !othersUnder ? <span ref={othersLine} className={styles.others} data-app-file-others>{othersText}</span> : null}
          <span className={styles.mark}>{name} · {t("appPlace.mark")}</span>
          {/* Notion's file preview head: "다운로드하기" and "페이지에 추가" before close (2026-11-06). */}
          <button type="button" className={styles.download} onClick={() => void downloadShown()} data-app-file-download>
            <Download aria-hidden="true" />{t("blockItem:file.previewDownload")}</button>
          <AddFileToPageButton pageId={shown.pageId} blockId={shown.blockId} workspaceId={shown.workspaceId}
            onLeave={async () => { if (!await mayClose()) return false; setLeaving(true); return true; }}
            onAdded={onClose} onRefused={() => setLeaving(false)} />
          <button ref={close} type="button" className={styles.close} aria-label={t("actions.close")} onClick={() => void requestClose()}><X aria-hidden="true" /></button>
        </header>
        {othersText && othersUnder ? <p className={styles.othersUnder} data-app-file-others>{othersText}</p> : null}
        {editingEnded ? <p className={styles.editingEnded} role="status" data-app-file-editing-ended>{t("appPlace.editingEnded")}</p> : null}
        {appGone && edits ? <p className={`${styles.editingEnded} ${styles.appGone}`} role="status" data-app-file-app-gone>
          <span>{t(notSaved ? "appPlace.appGoneKept" : "appPlace.appGone")}</span>
          {notSaved ? <button type="button" className={styles.download} onClick={downloadNotSaved} data-app-file-download-edits>
            <Download aria-hidden="true" />{t("appPlace.downloadEdits")}</button> : null}
        </p> : null}
        <AppPlaceFrame key={generation} session={session} usable={app} place="fileOpener"
          opens={{ surface: "fileOpener", opener: opener.key, pageId: shown.pageId, fileBlockId: shown.blockId }} title={name} style={{ flex: "1 1 auto", minHeight: 0, width: "100%", border: 0 }}
          context={() => ({ file: { ...shown } })} port={port} fileSave={fileSave} extra={extra} />
        {conflict ? <div ref={conflictBox} className={styles.conflict} role="alertdialog" aria-modal="true" aria-labelledby="app-file-conflict-title"
          tabIndex={-1} data-app-file-conflict>
          <div className={styles.conflictBox}>
            <h2 id="app-file-conflict-title" className={styles.conflictTitle}>
              {conflict.found.savedBy ? t("appPlace.saveConflictTitle", { name: conflict.found.savedBy }) : t("appPlace.saveConflictTitleUnknown")}</h2>
            <p className={styles.conflictBody}>{t("appPlace.saveConflictBody")}</p>
            {(["overwrite", "copy", "latest"] as const).map((choice) => (
              <button key={choice} type="button" className={styles.choice} data-app-file-choice={choice} onClick={() => settle(choice)}>
                <span className={styles.choiceName}>{t(`appPlace.saveChoice.${choice}`)}</span>
                <span className={styles.choiceHint}>{t(`appPlace.saveChoice.${choice}Hint`)}</span>
              </button>
            ))}
            <button type="button" className={styles.cancel} onClick={() => settle("cancel")}>{t("actions.cancel")}</button>
          </div>
        </div> : null}
      </div>
    </div>,
    document.body,
  );
}

/** A copy's name: the file's own with the language's "copy" before its extension. */
function copyNameOf(name: string, copy: (base: string) => string): string {
  const dot = name.lastIndexOf(".");
  return dot > 0 ? `${copy(name.slice(0, dot))}${name.slice(dot)}` : copy(name);
}
