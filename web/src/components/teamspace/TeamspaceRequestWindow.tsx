import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { teamspaceBounds } from "@/teamspaceWire.generated";
import { teamspaceGlyph } from "./KeptIconGlyph";
import styles from "./TeamspaceRequestWindow.module.css";

/**
 * The window a closed teamspace's "요청" opens, as Notion's (its code, read
 * 2026-11-17 — TeamInviteLinkRequestModal, which its teamspace button opens
 * for a closed teamspace the reader isn't in): 420 wide, 15 in, "〈icon〉
 * 〈팀〉 팀에 대한 액세스 요청" with 10 under it, and 15 lower the foot with
 * "팀스페이스 관리자가 요청을 승인하거나 거부할 수 있습니다." (small, grey; Notion's
 * 거절 in the screen words' 거부) and a
 * blue "요청" 20 after it. Between them "메시지(선택사항)", Notion's input
 * as a textarea (its code, read 2026-11-17, drawn with its own styles: 8
 * in, a 1px ring, three 20px lines — 76 tall); what it says goes with the
 * request to the teamspace's owners. The request is the server's to decide
 * first: the press shows busy at once, and the window closes once it is in.
 */
export function TeamspaceRequestWindow({ teamspace, onRequest, onClose }: {
  teamspace: { name: string; icon: string };
  onRequest(message: string): Promise<boolean>;
  onClose(): void;
}) {
  const { t } = useTranslation(["teamspaceDialog", "common"]);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onClose(); },
  });

  async function send() {
    if (busy) return;
    setBusy(true);
    const sent = await onRequest(message);
    setBusy(false);
    if (sent) onClose();
  }

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")}
        onClick={() => { if (!busy) onClose(); }} />
      <div ref={dialogRef} className={styles.window} role="dialog" aria-modal="true" aria-labelledby={titleId} data-teamspace-request-window>
        <p id={titleId} className={styles.title}>
          <Trans t={t} i18nKey="teamspaceDialog:requestWindow.title" values={{ name: teamspace.name }}
            components={{ icon: <RequestGlyph teamspace={teamspace} /> }} />
        </p>
        <div className={styles.messageField}>
          <textarea className={styles.message} rows={3} value={message} placeholder={t("teamspaceDialog:requestWindow.message")}
            aria-label={t("teamspaceDialog:requestWindow.message")} maxLength={teamspaceBounds.joinMessageRunes} disabled={busy}
            data-teamspace-request-message onChange={(event) => setMessage(event.target.value)} />
        </div>
        <div className={styles.foot}>
          <p className={styles.caption}>{t("teamspaceDialog:requestWindow.caption")}</p>
          <button type="button" className={styles.send} disabled={busy} aria-busy={busy || undefined}
            data-teamspace-request-send onClick={() => void send()}>
            {t("teamspaceDialog:requestWindow.send")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

// Its own component, so the title's translation, which has no words inside
// the icon's tag, leaves the glyph it draws in place.
function RequestGlyph({ teamspace }: { teamspace: { name: string; icon: string } }) {
  return <span className={styles.glyph} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>;
}
