import {
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { filePreviewWidth, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { usePageNames } from "@/lib/pageNames";
import { ArrowLineDownIcon } from "@/icons/leafen";
import { useProductToast } from "@/lib/productToast";
import type { Page } from "@/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { trapModalTab } from "@/lib/modalFocus";
import { CoverCredit } from "./CoverCredit";
import { PageCoverPicker } from "./PageCoverPicker";
import { coverImage, type PagePresentationChange } from "./pagePresentation";
import { coverDownloadable, downloadPageCover } from "./pageCoverDownload";
import { coverGradientImage } from "./coverGradient";
import styles from "./PageCover.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

function clampPosition(value: number) {
  return Math.max(0, Math.min(100, Math.round(value)));
}

export function PageCover({
  page,
  onChange,
  compact = false,
  database = false,
  readOnly = false,
  changeLabel,
}: {
  page: Page;
  onChange: (
    change: PagePresentationChange,
    basePage?: Page,
  ) => boolean | Page | void | Promise<boolean | Page | void>;
  compact?: boolean;
  /** A database's own page's, a wiki's too: 20% of the window, as Notion's. */
  database?: boolean;
  readOnly?: boolean;
  /** The change button's words where they differ from a page's (a profile's "변경하기"). */
  changeLabel?: string;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["pageCover", "common"]);
  const { notify } = useProductToast();
  const [repositioning, setRepositioning] = useState(false);
  const [draftPosition, setDraftPosition] = useState<number | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [presentationPending, setPresentationPending] = useState(false);
  const coverRef = useRef<HTMLDivElement>(null);
  const changeButtonRef = useRef<HTMLButtonElement>(null);
  const actionsRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ startY: number; startPosition: number; height: number } | null>(null);
  const downloading = useRef(false);
  const coverMenuId = useId();
  const menuRef = useRef<HTMLDivElement>(null);
  // Escape or a press outside closes it even while a chosen cover is still
  // saving: the save goes on and its answer still shows (a cover chosen and
  // Escape at once kept the window open till the save ended, 2026-11-09).
  // It floats over the whole window, the top bar and the sidebar too (pageHoverShield.ts): a press beside it only
  // closes it, the focus back on "변경하기", as Notion's (2026-11-26, Chrome 1440, two tabs, judged after the press:
  // a line's words, a to-do's box and the top bar's "공유" did nothing more, no hover look under the pointer).
  usePopoverAppear(menuOpen, menuRef);
  const menuLayer = useOverlayLayer(menuOpen, { kind: "menu", ref: menuRef,
    onEscape: () => closeCoverMenu(),
    onOutsidePress: () => closeCoverMenu(),
    inside: () => [changeButtonRef.current], returnFocusRef: changeButtonRef,
    canRestoreFocus: () => !presentationPending });
  // A cover spans the window.
  const resolvedCoverUrl = useWorkspaceFileUrl(page.cover, ["data:image/"], { width: filePreviewWidth(window.innerWidth) });


  useEffect(() => {
    if (!readOnly) return;
    const frame = window.requestAnimationFrame(() => {
      setMenuOpen(false);
      setRepositioning(false);
      setDraftPosition(null);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [readOnly]);

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      setMenuOpen(false);
      setRepositioning(false);
      setDraftPosition(null);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [page.id]);

  if (!page.cover) return null;

  const position = draftPosition ?? page.coverPosition ?? 50;
  // A gallery color or gradient moves as the image it stands for (coverGradient.ts).
  const canReposition = !page.cover.startsWith("linear-gradient") || coverGradientImage(page.cover) !== null;

  function closeCoverMenu() { setMenuOpen(false); }

  async function downloadCover() {
    if (!page.cover || downloading.current) return;
    downloading.current = true;
    try {
      const saved = await downloadPageCover(page.cover, resolvedCoverUrl, pageNames.title(page));
      if (!saved) notify(t("pageCover:toast.downloadFailed"), "error");
    } finally {
      downloading.current = false;
    }
  }

  async function applyCover(cover: string, keepPickerOpen = false) {
    if (readOnly || presentationPending) return false;
    setPresentationPending(true);
    try {
      const applied = await onChange({ cover, coverPosition: 50 });
      if (applied === false) return false;
      setDraftPosition(null);
      setRepositioning(false);
      if (!keepPickerOpen) closeCoverMenu();
      return true;
    } finally {
      setPresentationPending(false);
    }
  }

  async function removeCover() {
    if (readOnly || presentationPending) return;
    setPresentationPending(true);
    let removed: Awaited<ReturnType<typeof onChange>>;
    // Settled whichever way the save goes, so a cover added again can be
    // changed and removed again, as the other cover changes do.
    try {
      removed = await onChange({ cover: "", coverPosition: 50 });
    } finally {
      setPresentationPending(false);
    }
    if (removed === false) return;
    setDraftPosition(null);
    setRepositioning(false);
    setMenuOpen(false);
  }

  function startReposition() {
    if (readOnly) return;
    setDraftPosition(page.coverPosition ?? 50);
    closeCoverMenu();
    setRepositioning(true);
    window.requestAnimationFrame(() => coverRef.current?.focus());
  }

  async function savePosition() {
    if (readOnly || presentationPending) return;
    setPresentationPending(true);
    try {
      const saved = await onChange({ coverPosition: position });
      if (saved === false) return;
      setDraftPosition(null);
      setRepositioning(false);
      window.requestAnimationFrame(() => changeButtonRef.current?.focus());
    } finally {
      setPresentationPending(false);
    }
  }

  function cancelPosition() {
    setDraftPosition(null);
    setRepositioning(false);
    window.requestAnimationFrame(() => changeButtonRef.current?.focus());
  }

  function onPointerDown(event: React.PointerEvent<HTMLDivElement>) {
    if (!repositioning || (event.target as HTMLElement).closest("button")) return;
    event.preventDefault();
    const rect = event.currentTarget.getBoundingClientRect();
    drag.current = { startY: event.clientY, startPosition: position, height: Math.max(1, rect.height) };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function onPointerMove(event: React.PointerEvent<HTMLDivElement>) {
    const current = drag.current;
    if (!current) return;
    const delta = ((event.clientY - current.startY) / current.height) * 100;
    setDraftPosition(clampPosition(current.startPosition + delta));
  }

  function onCoverKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (!repositioning) return;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      cancelPosition();
      return;
    }
    if (event.key === "Tab") {
      trapModalTab(event, coverRef.current);
      return;
    }
    if (!["ArrowUp", "ArrowDown", "Home", "End", "PageUp", "PageDown", "Enter"].includes(event.key)) return;
    if ((event.target as HTMLElement).closest("[data-reposition-action]") && fieldEnter(event)) return;
    event.preventDefault();
    event.stopPropagation();
    if (fieldEnter(event)) {
      void savePosition();
      return;
    }
    const step = event.shiftKey || event.key === "PageUp" || event.key === "PageDown" ? 10 : 1;
    if (event.key === "ArrowUp" || event.key === "PageUp") setDraftPosition(clampPosition(position - step));
    if (event.key === "ArrowDown" || event.key === "PageDown") setDraftPosition(clampPosition(position + step));
    if (event.key === "Home") setDraftPosition(0);
    if (event.key === "End") setDraftPosition(100);
  }

  return (
    <div
      ref={coverRef}
      className={styles.cover}
      data-native-page-cover
      data-compact={compact ? "true" : undefined}
      data-database={database && !compact ? "true" : undefined}
      data-menu-open={menuOpen ? "true" : undefined}
      data-presentation-pending={presentationPending ? "true" : undefined}
      data-repositioning={repositioning ? "true" : undefined}
      tabIndex={repositioning || !readOnly ? 0 : undefined}
      role={repositioning ? "slider" : "group"}
      aria-label={repositioning
        ? t("pageCover:aria.coverVerticalPosition")
        : t("pageCover:aria.pageCover")}
      aria-valuemin={repositioning ? 0 : undefined}
      aria-valuemax={repositioning ? 100 : undefined}
      aria-valuenow={repositioning ? position : undefined}
      aria-valuetext={repositioning ? t("pageCover:aria.positionFromTop", { position }) : undefined}
      style={{
        backgroundImage: coverImage(page.cover, resolvedCoverUrl),
        backgroundPositionY: `${position}%`,
      }}
      onKeyDown={onCoverKeyDown}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={() => { drag.current = null; }}
      onPointerCancel={() => { drag.current = null; }}
    >
      {repositioning ? null : <CoverCredit cover={page.cover} />}
      {menuOpen && (
        <button
          type="button"
          className={styles.menuScrim}
          aria-label={t("pageCover:aria.closeCoverMenu")}
          tabIndex={-1}
          aria-disabled={presentationPending}
        />
      )}
      {!readOnly && !repositioning && (
        <div ref={actionsRef} className={styles.actions}>
          <button
            ref={changeButtonRef}
            type="button"
            aria-haspopup="dialog"
            aria-expanded={menuOpen}
            aria-controls={menuOpen ? coverMenuId : undefined}
            aria-label={t("pageCover:aria.changePageCover")}
            disabled={presentationPending}
            onClick={() => {
              if (menuOpen) return closeCoverMenu();
              setMenuOpen(true);
            }}
          >
            {changeLabel ?? t("pageCover:actions.changeCover")}
          </button>
          {canReposition && (
            <>
              <span className={styles.actionDivider} aria-hidden="true" />
              <button type="button" aria-label={t("pageCover:aria.repositionPageCover")} disabled={presentationPending} onClick={startReposition}>
                {t("pageCover:actions.reposition")}
              </button>
            </>
          )}
          {coverDownloadable(page.cover, resolvedCoverUrl) && (
            <>
              <span className={styles.actionDivider} aria-hidden="true" />
              <button type="button" className={styles.iconAction} aria-label={t("pageCover:aria.downloadCoverImage")}
                title={t("pageCover:aria.downloadCoverImage")} onClick={() => void downloadCover()}>
                <ArrowLineDownIcon size="small" aria-hidden="true" />
              </button>
            </>
          )}
          {menuOpen && (
            <PageCoverPicker
              key={page.id}
              currentCover={page.cover}
              dialogId={coverMenuId}
              anchorRef={actionsRef}
              busy={presentationPending}
              onApplyCover={applyCover}
              onRemoveCover={removeCover}
              menuRef={menuRef} zIndex={menuLayer.zIndex} onKeyDown={menuLayer.onKeyDown} onClose={closeCoverMenu}
              uploadTarget={{ workspaceId: page.workspaceId, pageId: page.id }}
            />
          )}
        </div>
      )}
      {repositioning && (
        <>
          <span className={styles.repositionHint}>{t("pageCover:reposition.hint")}</span>
          <div className={styles.actions}>
            <button type="button" data-reposition-action disabled={presentationPending} onClick={() => void savePosition()}>
              {t("pageCover:actions.savePosition")}
            </button>
            <button type="button" data-reposition-action disabled={presentationPending} onClick={cancelPosition}>
              {t("common:actions.cancel")}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
