"use client";

import {
  type RefObject,
  useEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { PageCoverGallery } from "./PageCoverGallery";
import { PageCoverUnsplash, UnsplashTabMark } from "./PageCoverUnsplash";
import { createFocusManager } from "@react-aria/focus";
import { FocusScope, useDialog } from "react-aria";
import { Tabs, TabList, Tab, TabPanel } from "react-aria-components";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import {
  uploadWorkspaceFile,
  type UploadProgress,
  type WorkspaceFileTarget,
} from "@/lib/storage";
import { confirmAction } from "../ConfirmActionDialog";
import { validateCoverImageLink } from "./validateCoverImageLink";
import { safeExternalPresentationImage } from "./pagePresentation";
import styles from "./PageCover.module.css";
import { uploadRefusalReason } from "@/lib/uploadRefusal";
import { waitingNoteText } from "@/lib/pendingChoice";
import { TouchPickerHead, touchPickerCard, useTouchPickerCard } from "@/components/shell/TouchPickerCard";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type CoverTab = "gallery" | "upload" | "link" | "unsplash";
type CoverUploadProgress = UploadProgress & { fileName: string };

const COVER_TABS: CoverTab[] = ["gallery", "upload", "link"];

export function PageCoverPicker({
  busy = false,
  currentCover,
  dialogId,
  anchorRef,
  onApplyCover,
  onRemoveCover,
  uploadTarget,
  menuRef,
  zIndex,
  onKeyDown,
  onClose,
}: {
  busy?: boolean;
  /** Closes the picker, from a touch screen's card's "닫기" (TouchPickerCard). */
  onClose: () => void;
  menuRef: RefObject<HTMLDivElement | null>;
  zIndex?: number;
  onKeyDown: React.KeyboardEventHandler<HTMLDivElement>;
  currentCover: string;
  dialogId: string;
  /** The cover toolbar whose right edge anchors the picker. */
  anchorRef: RefObject<HTMLElement | null>;
  onApplyCover: (cover: string, keepPickerOpen?: boolean) => Promise<boolean>;
  onRemoveCover: () => Promise<void>;
  uploadTarget: WorkspaceFileTarget;
}) {
  const { t } = useTranslation(["pageCover", "common"]);
  const [unsplashEnabled, setUnsplashEnabled] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void fetchRuntimeConfig().then(({ config }) => { if (!cancelled) setUnsplashEnabled(config.unsplashCoverEnabled === true); });
    return () => { cancelled = true; };
  }, []);
  const tabs = unsplashEnabled ? [...COVER_TABS, "unsplash" as const] : COVER_TABS;
  const [tab, setTab] = useState<CoverTab>("gallery");
  const [linkDraft, setLinkDraft] = useState("");
  const [checkingLink, setCheckingLink] = useState(false);
  const linkCheck = useRef<AbortController | null>(null);
  useEffect(() => () => linkCheck.current?.abort(), []);
  const [applying, setApplying] = useState(false);
  const [uploadError, setUploadError] = useState("");
  const [uploadProgress, setUploadProgress] = useState<CoverUploadProgress | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  // Under the button, lined up with its right edge, or over it where only
  // that side has room.
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(
    popoverRectOf(anchorRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
    { gap: 6, align: "end" },
  ), [anchorRef], undefined, false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const uploadAbortRef = useRef<AbortController | null>(null);
  const uploadGenerationRef = useRef(0);
  const { dialogProps } = useDialog({ "aria-label": t("pageCover:aria.changeCoverDialog") }, menuRef);
  // A touch screen wider than a phone's opens it as a card in the middle of
  // the screen, as Notion's iPad (TouchPickerCard), the card itself taking
  // the focus where no keys are to type with. Measured 2026-11-16 after the press only (no script in the page
  // before it), Notion's iPad Pro 11 Safari (two tabs 11 minutes apart): the cover's "변경하기" tapped, the card
  // showed with no caret and the keyboard down (Leafen the same).
  const card = useTouchPickerCard();
  const entered = useRef(false);
  useEffect(() => {
    if ((!place && !card) || entered.current) return;
    entered.current = true;
    if (card) menuRef.current?.focus({ preventScroll: true });
    else createFocusManager(menuRef).focusFirst({ tabbable: true });
  }, [Boolean(place), card, menuRef]);

  function cancelUpload() {
    uploadGenerationRef.current += 1;
    uploadAbortRef.current?.abort();
    uploadAbortRef.current = null;
  }

  useEffect(() => () => cancelUpload(), []);

  useEffect(() => {
    cancelUpload();
    setUploadProgress(null);
    setUploadError("");
  }, [uploadTarget.workspaceId, uploadTarget.pageId]);

  function isCurrentUpload(generation: number, controller: AbortController) {
    return uploadGenerationRef.current === generation
      && uploadAbortRef.current === controller
      && !controller.signal.aborted;
  }

  async function pickFile(file?: File) {
    if (!file) return;
    if (!file.type.startsWith("image/")) { setUploadError(t("pageCover:upload.invalid")); return; }
    if (applying || busy || uploadProgress) return;
    cancelUpload();
    const generation = uploadGenerationRef.current;
    const controller = new AbortController();
    uploadAbortRef.current = controller;
    const fileName = file.name || t("pageCover:upload.defaultFileName");
    try {
      setUploadError("");
      setUploadProgress({ phase: "preparing", percent: 0, fileName });
      const uploaded = await uploadWorkspaceFile(file, "covers", uploadTarget, {
        signal: controller.signal,
        onProgress: (progress) => {
          if (!isCurrentUpload(generation, controller)) return;
          setUploadProgress({ ...progress, fileName });
        },
      });
      if (!isCurrentUpload(generation, controller)) return;
      uploadAbortRef.current = null;
      setUploadProgress(null);
      await applyCover(uploaded.url);
    } catch (error) {
      if (!isCurrentUpload(generation, controller)) return;
      uploadAbortRef.current = null;
      setUploadProgress(null);
      setUploadError(uploadRefusalReason(error) ?? t("pageCover:upload.error"));
      setTab("upload");
    }
  }

  function showInvalidLink() {
    return confirmAction({ message: t("pageCover:link.unavailable"), confirmLabel: t("pageCover:link.acknowledge"), cancelLabel: null });
  }

  async function applyLink() {
    if (checkingLink || applying || busy || uploadProgress) return;
    const value = safeExternalPresentationImage(linkDraft);
    if (!value) {
      await showInvalidLink();
      return;
    }
    const controller = new AbortController();
    linkCheck.current?.abort(); linkCheck.current = controller;
    setCheckingLink(true);
    const valid = await validateCoverImageLink(value, controller.signal);
    if (controller.signal.aborted) return;
    setCheckingLink(false);
    if (!valid) { await showInvalidLink(); return; }
    await applyCover(value);
  }

  async function applyCover(cover: string, keepPickerOpen = false) {
    if (applying || busy) return false;
    setApplying(true);
    cancelUpload();
    try {
      return await onApplyCover(cover, keepPickerOpen);
    } finally {
      setApplying(false);
    }
  }

  function uploadProgressLabel(progress: CoverUploadProgress) {
    if (progress.waiting) return waitingNoteText();
    if (progress.phase === "preparing") return t("pageCover:progress.preparing");
    if (progress.phase === "finalizing") return t("pageCover:progress.finalizing");
    if (progress.phase === "complete") return t("pageCover:progress.complete");
    return t("pageCover:progress.uploading");
  }

  const pending = applying || busy || checkingLink || Boolean(uploadProgress);
  return (
    <PopoverLayer>{card ? <div className={touchPickerCard.shade} style={{ zIndex }} aria-hidden="true" /> : null}<div
      {...dialogProps}
      id={dialogId}
      ref={menuRef}
      className={card ? `${styles.menu} ${touchPickerCard.card}` : styles.menu}
      data-touch-card={card ? "true" : undefined}
      tabIndex={card ? -1 : undefined}
      style={card ? { zIndex } : {
        position: "fixed", right: "auto", bottom: "auto", width: 540,
        // The box keeps its gallery's scroll: both sides hidden, never the
        // shorthand beside the placement's overflowY (React drew one over the other).
        ...popoverPlacementStyle(place), overflowX: "hidden", overflowY: "hidden", zIndex,
      }}
      onKeyDown={onKeyDown}
    >
      <FocusScope contain>
      {card ? <TouchPickerHead title={t("pageCover:picker.title")} removeLabel={t("pageCover:actions.remove")}
        onRemove={() => void onRemoveCover()} removeDisabled={pending} onClose={onClose} /> : null}
      <Tabs className={styles.sources} keyboardActivation="manual" selectedKey={tab} onSelectionChange={key => setTab(key as CoverTab)} isDisabled={applying || busy || checkingLink || Boolean(uploadProgress)}>
        <div className={card ? `${styles.menuHeader} ${touchPickerCard.tabRow}` : styles.menuHeader}>
        <div onKeyDown={event => {
          if (event.key === "ArrowDown" && (event.target as Element).closest('[role="tab"]')) {
            event.preventDefault(); createFocusManager(panelRef).focusFirst({ tabbable: true });
          }
        }}>
        <TabList className={styles.tabs} aria-label={t("pageCover:aria.coverSource")}>
          {tabs.map(value => <Tab key={value} id={value}
            data-cover-tab={value} data-cover-menu-item className={styles.coverTab}
            onPress={event => {
              if (event.pointerType !== "keyboard" && value !== "gallery") createFocusManager(panelRef).focusFirst({ tabbable: true });
            }}>
            {value === "unsplash" ? <UnsplashTabMark /> : null}
            {t(`pageCover:tabs.${value}`)}
          </Tab>)}
        </TabList>
        </div>
        {card ? null : <button type="button" className={styles.removeButton} aria-label={t("pageCover:aria.removePageCover")}
          disabled={pending} onClick={() => void onRemoveCover()}>
          {t("pageCover:actions.remove")}
        </button>}
        </div>
        <TabPanel className={styles.panel} key={tab} id={tab} ref={panelRef} data-cover-panel>
      {tab === "gallery" && <PageCoverGallery currentCover={currentCover} busy={applying || busy || Boolean(uploadProgress)}
        onApplyCover={cover => void applyCover(cover, true)} />}
      {tab === "unsplash" && <PageCoverUnsplash currentCover={currentCover} busy={applying || busy || Boolean(uploadProgress)} onApplyCover={cover => applyCover(cover, true)} />}

      {tab === "upload" && (
        <div
          className={styles.uploadPane}
          data-cover-panel
        >
          <input
            ref={fileInputRef}
            type="file"
            tabIndex={-1}
            aria-hidden="true"
            accept="image/*"
            onChange={(event) => {
              const file = event.currentTarget.files?.[0];
              event.currentTarget.value = "";
              void pickFile(file);
            }}
          />
          <button
            type="button"
            data-cover-menu-item
            aria-label={t("pageCover:aria.uploadFromDevice")}
            disabled={Boolean(uploadProgress) || applying || busy}
            aria-busy={Boolean(uploadProgress) || applying || busy}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploadProgress ? t("pageCover:actions.uploading") : t("pageCover:actions.uploadFile")}
          </button>
          {uploadProgress && (
            <div className={styles.uploadProgress} role="status" aria-live="polite">
              <div className={styles.uploadProgressHeader}>
                <strong>{uploadProgressLabel(uploadProgress)}</strong>
                {uploadProgress.waiting ? null : <span>{uploadProgress.percent}%</span>}
              </div>
              <div className={styles.uploadProgressName}>{uploadProgress.fileName}</div>
              <div
                className={styles.uploadProgressTrack}
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={uploadProgress.percent}
                aria-label={t("pageCover:aria.uploadingFile", { fileName: uploadProgress.fileName })}
              >
                <span style={{ width: `${uploadProgress.percent}%` }} />
              </div>
            </div>
          )}
          <p>{t("pageCover:upload.idealSize")}</p>
          {uploadError && <div role="alert" className={styles.linkError}>{uploadError}</div>}
        </div>
      )}

      {tab === "link" && (
        <div
          className={styles.linkPane}
          data-cover-panel
        >
          <input
            value={linkDraft}
            aria-label={t("pageCover:aria.coverImageLink")}
            placeholder={t("pageCover:link.placeholder")}
            onChange={(event) => {
              linkCheck.current?.abort(); setCheckingLink(false);
              setLinkDraft(event.target.value);
            }}
            onKeyDown={(event) => {
              if (fieldEnter(event)) void applyLink();
            }}
          />
          <button type="button" data-cover-menu-item disabled={applying || busy || checkingLink} aria-busy={checkingLink} onClick={() => void applyLink()}>
            {t("pageCover:actions.submit")}
          </button>
          {checkingLink && <p role="status">{t("pageCover:link.checking")}</p>}
          <p className={styles.linkHint}>{t("pageCover:link.hint")}</p>
        </div>
      )}
        </TabPanel>
      </Tabs>
      </FocusScope>
    </div></PopoverLayer>
  );
}
