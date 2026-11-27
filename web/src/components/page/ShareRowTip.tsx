import { type MouseEvent, type ReactNode, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { placeRowTip, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useTipPress } from "@/lib/tipTiming";
import { publicMutationRoles, type PublicMutationRole } from "@/publicMutationWire.generated";
import styles from "./ShareRowTip.module.css";

/**
 * What a share window row's access means, in a dark tip while the pointer is
 * over its name, as Notion's (seen 2026-10-13): beside the row where the
 * window has room, else under its middle. It goes as the app's other tips go
 * (lib/tipTiming.ts): off the name or with a press anywhere.
 */
export function ShareRowTip({ text, className, children }: { text: ReactNode; className?: string; children: ReactNode }) {
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const tipRef = useRef<HTMLDivElement>(null), own = useRef<HTMLSpanElement>(null);
  const show = (event: MouseEvent<HTMLElement>) =>
    setAnchor((event.currentTarget.closest("[data-share-row]") ?? event.currentTarget).getBoundingClientRect());
  const hide = () => setAnchor(null);
  useTipPress(anchor !== null, hide, () => own.current);
  const place = usePopoverPlacement(tipRef, anchor ? (size, viewport) => placeRowTip(anchor, size, viewport) : null,
    [anchor], undefined, false);
  return <span ref={own} className={className} onMouseEnter={show} onMouseLeave={hide}>
    {children}
    {anchor && typeof document !== "undefined" ? createPortal(
      <div ref={tipRef} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-share-row-tip>
        <span className={styles.text}>{text}</span></div>,
      document.body) : null}
  </span>;
}

/** A page named in a tip, with its icon, as Notion's. */
export function ShareRowTipPage({ icon, children }: { icon?: ReactNode; children?: ReactNode }) {
  return <strong className={styles.page}>{icon ? <span className={styles.pageIcon} aria-hidden="true">{icon}</span> : null}{children}</strong>;
}

/** "〈name〉 님은 이 페이지를 …" for a person's own role on the page. */
export function useShareRoleTip() {
  const { t } = useTranslation("topBar");
  return (name: string, role: PublicMutationRole) => t(
    role === publicMutationRoles.fullAccess ? "rowTipFullAccess" : role === publicMutationRoles.edit ? "rowTipEdit"
      : role === publicMutationRoles.comment ? "rowTipComment" : "rowTipView", { name });
}
