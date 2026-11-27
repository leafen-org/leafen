import { type ReactNode, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { useOverlayLayer } from "@/lib/overlayLayer";
import local from "./SettingsRows.module.css";

/** A settings window's small form over it: a title, then what the caller fills in. */
export function SettingsFormDialog({ title, busy, wide, initialFocus, onClose, children }: {
  title: string;
  busy: boolean;
  wide?: boolean;
  initialFocus: React.RefObject<HTMLElement | null>;
  onClose: () => void;
  children: ReactNode;
}) {
  const { t } = useTranslation("common");
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current],
    onEscape: () => { if (!busy) onClose(); }, initialFocus,
  });
  return createPortal(
    <div ref={overlayRef} className={dialogs.overlay} style={{ zIndex }}>
      <button type="button" className={dialogs.backdrop} tabIndex={-1} aria-label={t("actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={`${dialogs.dialog} ${wide ? local.dialogWide : local.narrowDialog}`}
        role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={dialogs.dialogTitle}>{title}</h2>
        {children}
      </section>
    </div>,
    document.body,
  );
}
