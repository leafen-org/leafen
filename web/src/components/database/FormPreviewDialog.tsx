import { useEffect, useLayoutEffect, useRef, type ReactNode, type Ref } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useProductPages } from "@/components/product/productPagesContext";
import { CopyTipButton } from "@/components/CopyTipButton";
import { IconTipButton } from "@/components/IconTipButton";
import { LinkIcon, OpenAsPage, Pencil, Star, StarFilled } from "@/icons/leafen";
import { useFormFavorites } from "@/lib/formFavorites";
import { formHref } from "@/lib/formViewModel";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { pushRoute } from "@/lib/router";
import { phoneScreen } from "@/lib/screenKind";
import styles from "./FormPreviewDialog.module.css";

/**
 * "미리보기", as Notion's opens it (seen 2026-10-08, 2026-11-04): the form as
 * its respondents see it, in a page centred over the builder (72px from the
 * window's top and bottom, at most 975 wide, round 12, over 60% black), with
 * "양식 편집" back to the builder, "양식 링크 복사", "즐겨찾기" and
 * "전체 페이지로 열기" in its top right. A button opens a form over the page
 * in the same window (Notion's, seen 2026-11-05).
 */
export function FormPreviewDialog({ children, token, onClose, onEdit, onCopyLink, onOpenFull, focusEntersWindow = false }: {
  children: ReactNode; onClose: () => void;
  /** "양식 링크 복사": the link it copies, read at the press ("" while there is none to give). */
  onCopyLink?: () => string | Promise<string>;
  /** The window takes the focus on its first button (a button's form); the builder's "미리보기" keeps it, as Notion's. */
  focusEntersWindow?: boolean;
  /** The form's link once it has one, which its "즐겨찾기" keeps. */
  token?: string;
  /** "양식 편집": back to the builder (closing it, by default), or none for one who can't edit the form. */
  onEdit?: (() => void) | null;
  /** Opens the form as its page, as Notion's preview does (seen 2026-11-04). */
  onOpenFull?: () => void;
}) {
  const { t } = useTranslation(["formView", "databaseView"]);
  const backdropRef = useRef<HTMLDivElement>(null);
  const firstRef = useRef<HTMLButtonElement>(null);
  // A phone's builder "미리보기" is the form's own page, as its link opens it
  // (Notion's phone, seen 2026-11-15 at 411: no window, the respondent page
  // under the app's bar); a form with no link yet, and a button's form, keep
  // the window.
  const toPage = !focusEntersWindow && Boolean(token) && phoneScreen();
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useLayoutEffect(() => {
    if (!toPage || !token) return;
    closeRef.current();
    pushRoute(formHref(token));
  }, [toPage, token]);
  const { zIndex } = useOverlayLayer(!toPage, {
    kind: "dialog", ref: backdropRef, modalOwned: () => [backdropRef.current], restoreFocus: true, onEscape: () => onClose(),
    initialFocus: focusEntersWindow ? undefined : "opener",
  });
  // Notion's builder "미리보기" leaves the focus on itself behind the window: judged after the press only (no script in the page
  // before it), one letter typed then went nowhere and the focus read after was "미리보기" — Chrome 1440, 2026-11-15 twice, in
  // tabs hours apart, the second on a form made by hand. Elsewhere (a button's form) the window takes it on its first button.
  useEffect(() => {
    if (!focusEntersWindow) return undefined;
    const frame = window.requestAnimationFrame(() => (firstRef.current ?? backdropRef.current)?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [focusEntersWindow]);
  if (toPage) return null;
  const dialog = (
    // The backdrop click is a pointer shortcut; the close button and Escape
    // provide equivalent keyboard paths.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div ref={backdropRef} className={styles.previewBackdrop} style={{ zIndex }} role="dialog" aria-modal="true"
      aria-label={t("preview")} tabIndex={-1} data-form-preview-dialog
      onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <div className={styles.previewPage}>
        <FormPreviewActions firstRef={firstRef} token={token} onEdit={onEdit === undefined ? onClose : onEdit}
          onCopyLink={onCopyLink} onOpenFull={onOpenFull} />
        {/* The page scrolls under its actions, which stay in its corner. */}
        <div className={styles.previewScroll}>{children}</div>
      </div>
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}

/**
 * A form page's actions in its top right, in one pill as Notion's (seen
 * 2026-11-05: 2px round its 24px buttons, round 6, ringed): "양식 편집",
 * "양식 링크 복사", "즐겨찾기" and "전체 페이지로 열기", each when it applies,
 * each named in a dark tip over it, the link's saying "복사됨" once copied
 * (seen 2026-11-15, CopyTipButton).
 */
export function FormPreviewActions({ token, onEdit, onCopyLink, onOpenFull, firstRef, className }: {
  token?: string; onEdit?: (() => void) | null; onCopyLink?: () => string | Promise<string>; onOpenFull?: () => void;
  firstRef?: Ref<HTMLButtonElement>; className?: string;
}) {
  const { t } = useTranslation(["formView", "databaseView"]);
  const pages = useProductPages();
  const { favorites, setFavorite } = useFormFavorites(pages?.session, pages?.workspaceId);
  const favorite = favorites.some((item) => item.token === token);
  // None to give, no pill (a form that takes no answers of theirs).
  if (!onEdit && !onCopyLink && !token && !onOpenFull) return null;
  return (
    <div className={`${styles.previewActions} ${className ?? ""}`}>
      {onEdit ? (
        <IconTipButton ref={firstRef} className={styles.previewAction} label={t("editForm")} onClick={onEdit}>
          <Pencil size="small" aria-hidden="true" />
        </IconTipButton>
      ) : null}
      {onCopyLink ? (
        <CopyTipButton className={styles.previewAction} label={t("previewCopyLink")} text={onCopyLink} copied={t("linkCopiedTip")} failed={t("copyLinkFailed")}>
          <LinkIcon size="small" aria-hidden="true" />
        </CopyTipButton>
      ) : null}
      {token ? (
        <IconTipButton className={styles.previewAction} label={t("favorite")} aria-pressed={favorite}
          data-form-preview-favorite onClick={() => void setFavorite(token, !favorite)}>
          {favorite ? <StarFilled size="small" aria-hidden="true" /> : <Star size="small" aria-hidden="true" />}
        </IconTipButton>
      ) : null}
      {onOpenFull ? (
        <IconTipButton className={styles.previewAction} label={t("databaseView:openAsFullPage")} onClick={onOpenFull}>
          <OpenAsPage size="small" aria-hidden="true" />
        </IconTipButton>
      ) : null}
    </div>
  );
}
