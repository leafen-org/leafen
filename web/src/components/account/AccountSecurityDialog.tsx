import { type ReactNode, useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import styles from "./AccountEmailManagement.module.css";
import cardStyles from "./AccountCardDialog.module.css";
import formStyles from "./AccountSecurityDialog.module.css";

/** A dialog drawn as Notion's account cards: its mark, grey words under the title, its width, and a corner × where it has no cancel button. */
export type AccountCard = { mark: ReactNode; words?: ReactNode; width?: number; closable?: boolean; tone?: "danger" };

const FIELD = "input:not([readonly]):not([disabled]):not([type=hidden]), textarea:not([readonly]):not([disabled])";

/**
 * Account action inputs use the shared modal layer's keyboard and focus
 * owner. Focus goes to what the person answers (2026-10-28: the two-step
 * dialog opened on itself, its code coming after, and a wrong code sent focus
 * to "키 복사"): a field marked wrong, else the field last typed in, else the
 * first field, else the first button — as the dialog opens, as its content
 * comes, and after each request.
 */
export function AccountSecurityDialog({ title, busy, wide = false, card, onClose, children }: {
  title: string; busy: boolean; wide?: boolean; card?: AccountCard; onClose(): void; children: ReactNode;
}) {
  const { t } = useTranslation("common");
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const typed = useRef<HTMLElement | null>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onClose(); } });
  useEffect(() => {
    const root = dialogRef.current;
    if (!root) return undefined;
    if (busy) { root.focus(); return undefined; }
    const enter = () => {
      const active = document.activeElement;
      if (active && active !== root && active !== document.body && root.contains(active)) return true;
      const target = root.querySelector<HTMLElement>("[aria-invalid=true]:not([disabled])")
        ?? (typed.current?.isConnected && root.contains(typed.current) && !typed.current.matches(":disabled") ? typed.current : null)
        ?? root.querySelector<HTMLElement>(FIELD) ?? root.querySelector<HTMLElement>("button:not([disabled])");
      (target ?? root).focus();
      return Boolean(target);
    };
    if (enter()) return undefined;
    // Content that comes after the dialog (the two-step setup's code) takes the focus as it comes.
    const watch = new MutationObserver(() => { if (enter()) watch.disconnect(); });
    watch.observe(root, { childList: true, subtree: true });
    const stop = window.setTimeout(() => watch.disconnect(), 5000);
    return () => { watch.disconnect(); window.clearTimeout(stop); };
  }, [busy]);
  return createPortal(<div className={styles.backdrop} style={{ zIndex }}
    onMouseDown={(event) => {
      if (event.target !== event.currentTarget || busy) return;
      event.preventDefault(); onClose();
    }}>
    <section ref={dialogRef} className={`${styles.dialog} ${formStyles.dialog} ${card ? cardStyles.card : ""}`} data-wide={wide || undefined}
      data-closable={card?.closable || undefined} style={card?.width ? { width: card.width } : undefined}
      role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy} tabIndex={-1}
      onFocus={(event) => { if (event.target.matches(FIELD)) typed.current = event.target; }}
      // A refusal made here, without a request (an empty or unmatched
      // field), also takes the focus to the field it marks (2026-10-29: Enter
      // in the new password left the empty confirmation marked behind it).
      onSubmitCapture={() => focusOpenedField(() => {
        const root = dialogRef.current;
        return !root || root.getAttribute("aria-busy") === "true" ? null : root.querySelector<HTMLElement>("[aria-invalid=true]:not([disabled])");
      })}>
      {card?.closable ? <button type="button" className={cardStyles.close} aria-label={t("actions.close")} disabled={busy} onClick={onClose}>
        <X size="tiny" aria-hidden="true" /></button> : null}
      {card ? <span className={cardStyles.mark} data-tone={card.tone} aria-hidden="true">{card.mark}</span> : null}
      <h2 id={titleId} className={card ? cardStyles.title : undefined}>{title}</h2>
      {card?.words ? <p className={cardStyles.words}>{card.words}</p> : null}
      {children}
    </section>
  </div>, document.body);
}
