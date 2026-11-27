import { useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { AltBadgeIcon, X } from "@/icons/leafen";
import { popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import styles from "./editorStyles.generated.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { giveFocusBack } from "@/lib/focusGiveBack";

/**
 * "ALT" at the foot's end of a picture that has alt text, as Notion's (seen
 * 2026-11-12): drawn as the picture's tools are, there without the pointer
 * on the picture; its tip is the alt text over a line offering a click to see
 * and edit it, and a press opens the alt text window over it.
 */
export function ImageAltBadge({ alt, buttonRef, onOpen }: { alt: string; buttonRef: RefObject<HTMLButtonElement | null>; onOpen: () => void }) {
  const { t } = useTranslation("blockHandle");
  return <span className={styles.imageAltBadge} data-image-alt-badge contentEditable={false}>
    <button ref={buttonRef} type="button" aria-label={t("altText.badgeHint")} data-icon-tip={alt} data-tip-keys={t("altText.badgeHint")}
      onPointerDown={(event) => event.stopPropagation()} onClick={(event) => { event.stopPropagation(); onOpen(); }}>
      <AltBadgeIcon size="small" aria-hidden="true" />
    </button>
  </span>;
}

/** Where the alt text window opens: over the badge's button, its end 8px past
 * it, or with its top start at a point (the block menu's "추가 옵션" it came from). */
export type ImageAltTextAnchor = { badge: Element } | { at: PopoverRect };

/**
 * A picture's alt text, as Notion's window (seen 2026-11-12): 310px, why
 * alt text helps over its field, "대체 텍스트 추가" when empty, a close
 * button at its top end. Enter, Escape, a press beside or the close button
 * keep what is written and close it; written empty, the picture has none.
 */
export function ImageAltTextWindow({ anchor, alt, onSave, onClose }: {
  anchor: ImageAltTextAnchor; alt: string; onSave: (text: string) => void; onClose: () => void;
}) {
  const { t } = useTranslation(["blockHandle", "common"]);
  const ref = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null);
  const [text, setText] = useState(alt);
  const done = useRef(false);
  // Closed, it gives the focus back to the badge it opened from, as Notion's (2026-11-15, Chrome, no script
  // before the action; judged by the screen and one typed letter: ALT pressed, Escape — the badge ringed with
  // its tip, the letter landing nowhere; 2026-11-26, two tabs: a press beside it or × — the badge unringed,
  // the letter nowhere). A window opened as it closes keeps the focus (Escape and ⌘⇧O at once, the same day).
  const badge = useRef<HTMLElement | null>("badge" in anchor && anchor.badge instanceof HTMLElement ? anchor.badge : null);
  const close = () => {
    if (done.current) return;
    done.current = true;
    if (text.trim() !== alt) onSave(text);
    onClose();
    const back = badge.current;
    if (back) giveFocusBack({ to: back, keepScroll: true, unlessOpened: true });
  };
  const place = usePopoverPlacement(ref, () => "badge" in anchor
    ? { anchor: popoverRectOf(anchor.badge) ?? { top: 0, bottom: 0, left: 0, right: 0 }, placement: "top-end", gap: 8, offsetTop: 8, options: {} }
    : { anchor: anchor.at, placement: "bottom-start", gap: 0, options: {} }, [anchor], undefined,
  { kind: "picker", onEscape: close, onOutsidePress: close });
  useEffect(() => {
    if (!place) return;
    const field = inputRef.current;
    field?.focus();
    field?.setSelectionRange(field.value.length, field.value.length);
  }, [Boolean(place)]); // eslint-disable-line react-hooks/exhaustive-deps
  return createPortal(<div ref={ref} role="dialog" aria-label={t("blockHandle:altText.title")} className={styles.imageAltWindow}
    style={{ ...popoverPlacementStyle(place), visibility: place ? "visible" : "hidden" }} data-image-alt-window>
    <div className={styles.imageAltHelp}>
      <button type="button" className={styles.imageAltClose} aria-label={t("common:actions.close")} onClick={close}>
        <X size="small" aria-hidden="true" />
      </button>
      {t("blockHandle:altText.windowHelp")}
    </div>
    <input ref={inputRef} className={styles.imageAltField} value={text} maxLength={1000} placeholder={t("blockHandle:altText.windowPlaceholder")}
      aria-label={t("blockHandle:altText.title")} onChange={(event) => setText(event.currentTarget.value)}
      onKeyDown={(event) => { if (fieldEnter(event)) { event.preventDefault(); close(); } }} />
  </div>, document.body);
}
